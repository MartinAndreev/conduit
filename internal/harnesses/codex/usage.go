package codex

import "github.com/MartinAndreev/conduit/internal/sessions/provider"

// nativeTokenBreakdown mirrors Codex TokenUsageBreakdown. Pointers keep a
// counter absent from the payload (cacheWriteInputTokens is optional in the
// schema) distinct from a reported zero, so it is reported as unknown.
type nativeTokenBreakdown struct {
	InputTokens           *uint64 `json:"inputTokens"`
	CachedInputTokens     *uint64 `json:"cachedInputTokens"`
	CacheWriteInputTokens *uint64 `json:"cacheWriteInputTokens"`
	OutputTokens          *uint64 `json:"outputTokens"`
	ReasoningOutputTokens *uint64 `json:"reasoningOutputTokens"`
}

func (b nativeTokenBreakdown) counters() provider.UsageCounters {
	return provider.UsageCounters{
		InputTokens:      optionalCount(b.InputTokens),
		OutputTokens:     optionalCount(b.OutputTokens),
		ReasoningTokens:  optionalCount(b.ReasoningOutputTokens),
		CacheReadTokens:  optionalCount(b.CachedInputTokens),
		CacheWriteTokens: optionalCount(b.CacheWriteInputTokens),
	}
}

// since subtracts a baseline counter by counter. ok is false when any reported
// counter is below its baseline, which means the thread-wide total regressed.
func (b nativeTokenBreakdown) since(base usageTotals) (provider.UsageCounters, bool) {
	ok := true
	minus := func(value *uint64, baseline uint64) provider.Optional[uint64] {
		if value == nil {
			return provider.None[uint64]()
		}
		if *value < baseline {
			ok = false
			return provider.None[uint64]()
		}
		return provider.Some(*value - baseline)
	}
	counters := provider.UsageCounters{
		InputTokens:      minus(b.InputTokens, base.input),
		OutputTokens:     minus(b.OutputTokens, base.output),
		ReasoningTokens:  minus(b.ReasoningOutputTokens, base.reasoning),
		CacheReadTokens:  minus(b.CachedInputTokens, base.cachedInput),
		CacheWriteTokens: minus(b.CacheWriteInputTokens, base.cacheWrite),
	}
	return counters, ok
}

func optionalCount(value *uint64) provider.Optional[uint64] {
	if value == nil {
		return provider.None[uint64]()
	}
	return provider.Some(*value)
}

// usageTotals is a thread-wide arithmetic baseline. A counter never reported
// counts as zero here (the schema default); it is never emitted as known.
type usageTotals struct {
	input, cachedInput, cacheWrite, output, reasoning uint64
}

// merge returns the totals with every reported counter replaced.
func (t usageTotals) merge(b nativeTokenBreakdown) usageTotals {
	set := func(target *uint64, value *uint64) {
		if value != nil {
			*target = *value
		}
	}
	set(&t.input, b.InputTokens)
	set(&t.cachedInput, b.CachedInputTokens)
	set(&t.cacheWrite, b.CacheWriteInputTokens)
	set(&t.output, b.OutputTokens)
	set(&t.reasoning, b.ReasoningOutputTokens)
	return t
}

func (t usageTotals) plus(o usageTotals) usageTotals {
	return usageTotals{
		input: t.input + o.input, cachedInput: t.cachedInput + o.cachedInput,
		cacheWrite: t.cacheWrite + o.cacheWrite, output: t.output + o.output,
		reasoning: t.reasoning + o.reasoning,
	}
}

// minusClamped subtracts per counter and floors each result at zero.
func (t usageTotals) minusClamped(o usageTotals) usageTotals {
	sub := func(a, b uint64) uint64 {
		if a < b {
			return 0
		}
		return a - b
	}
	return usageTotals{
		input: sub(t.input, o.input), cachedInput: sub(t.cachedInput, o.cachedInput),
		cacheWrite: sub(t.cacheWrite, o.cacheWrite), output: sub(t.output, o.output),
		reasoning: sub(t.reasoning, o.reasoning),
	}
}

// usageTracker derives per-turn usage from Codex's thread-wide totals. Codex
// thread/tokenUsage/updated reports tokenUsage.total for the whole thread, so
// the active turn's cumulative snapshot is total minus the thread total seen
// when that turn became active. Before any total is observed the baseline is
// zero (a new thread). It is owned by the session mutex.
type usageTracker struct {
	thread   usageTotals // latest thread-wide total observed
	baseline usageTotals // thread total when the active turn became active
}

// beginTurn captures the baseline; call it where the turn becomes active.
func (t *usageTracker) beginTurn() { t.baseline = t.thread }

// observe records one thread-wide total. For the active turn it returns the
// turn's counters and measurement; regressed reports a total below the
// baseline. Then a per-turn cumulative value cannot be derived, so the
// native `last` breakdown (the newest model response) is returned with an
// unknown measurement, and the baseline is rebased so that later snapshots
// continue from it instead of repeating the regression.
func (t *usageTracker) observe(total, last nativeTokenBreakdown, activeTurn bool) (provider.UsageCounters, provider.UsageMeasurement, bool) {
	previous := t.thread
	t.thread = previous.merge(total)
	if !activeTurn {
		// A late total for another turn grows the thread total without
		// belonging to the active turn; move the baseline past that growth.
		t.baseline = t.baseline.plus(t.thread.minusClamped(previous))
		return provider.UsageCounters{}, provider.UsageMeasurementUnknown, false
	}
	if counters, ok := total.since(t.baseline); ok {
		return counters, provider.UsageCumulativeSnapshot, false
	}
	t.baseline = t.thread.minusClamped(usageTotals{}.merge(last))
	return last.counters(), provider.UsageMeasurementUnknown, true
}
