import { test } from "bun:test";
import assert from "node:assert/strict";
import { CommandBus } from "../../src/system/bus/command-bus.js";
import type {
  Command,
  CommandHandler,
} from "../../src/system/bus/command-bus.js";

interface TestCommand extends Command {
  readonly type: "test";
  readonly value: number;
}

interface TestResult {
  readonly doubled: number;
}

test("CommandBus dispatches to registered handler", async () => {
  const bus = new CommandBus();
  const handler: CommandHandler<TestCommand, TestResult> = async (cmd) => ({
    success: true,
    data: { doubled: cmd.value * 2 },
  });
  bus.register<TestCommand, TestResult>("test", handler);
  const result = await bus.dispatch<TestCommand, TestResult>({
    type: "test",
    value: 5,
  });
  assert.equal(result.success, true);
  if (result.success) assert.equal(result.data.doubled, 10);
});

test("CommandBus rejects duplicate handler registration", () => {
  const bus = new CommandBus();
  const handler: CommandHandler<TestCommand, TestResult> = async (cmd) => ({
    success: true,
    data: { doubled: cmd.value },
  });
  bus.register<TestCommand, TestResult>("test", handler);
  assert.throws(
    () => bus.register<TestCommand, TestResult>("test", handler),
    /Duplicate command handler registration: test/,
  );
});

test("CommandBus returns error for unregistered command type", async () => {
  const bus = new CommandBus();
  const result = await bus.dispatch<TestCommand, TestResult>({
    type: "test",
    value: 1,
  });
  assert.equal(result.success, false);
  if (!result.success) assert.equal(result.error.code, "HANDLER_NOT_FOUND");
});

test("CommandBus catches handler exceptions", async () => {
  const bus = new CommandBus();
  bus.register<TestCommand, TestResult>("test", async () => {
    throw new Error("boom");
  });
  const result = await bus.dispatch<TestCommand, TestResult>({
    type: "test",
    value: 1,
  });
  assert.equal(result.success, false);
  if (!result.success) {
    assert.equal(result.error.code, "HANDLER_ERROR");
    assert.match(result.error.message, /boom/);
  }
});

test("CommandBus handles non-Error thrown values", async () => {
  const bus = new CommandBus();
  bus.register<TestCommand, TestResult>("test", async () => {
    throw "string error";
  });
  const result = await bus.dispatch<TestCommand, TestResult>({
    type: "test",
    value: 1,
  });
  assert.equal(result.success, false);
  if (!result.success) assert.equal(result.error.code, "HANDLER_ERROR");
});
