package provider_test

import (
	"context"

	"github.com/MartinAndreev/conduit/internal/sessions/provider"
)

type fakeProvider struct {
	session *fakeSession
}

func (f *fakeProvider) Inspect(context.Context, provider.InspectRequest) (provider.ProviderDescriptor, error) {
	return provider.ProviderDescriptor{ProviderName: "fake"}, nil
}

func (f *fakeProvider) OpenSession(context.Context, provider.OpenSessionRequest) (provider.OpenSessionResult, error) {
	return provider.OpenSessionResult{Session: f.session}, nil
}

func (f *fakeProvider) ResumeSession(context.Context, provider.ResumeSessionRequest) (provider.OpenSessionResult, error) {
	return provider.OpenSessionResult{Session: f.session}, nil
}

type fakeSession struct{}

func (*fakeSession) Ref() provider.SessionRef { return provider.SessionRef{} }

func (*fakeSession) Capabilities() provider.CapabilitySnapshot {
	return provider.CapabilitySnapshot{}
}

func (*fakeSession) Events(context.Context, provider.EventCursor) (provider.EventStream, error) {
	return fakeEventStream{}, nil
}

func (*fakeSession) ObserveState(context.Context, provider.ObserveStateRequest) (provider.SessionSnapshot, error) {
	return provider.SessionSnapshot{}, nil
}

func (*fakeSession) StartTurn(context.Context, provider.StartTurnRequest) (provider.StartTurnResult, error) {
	return provider.StartTurnResult{}, nil
}

func (*fakeSession) Steer(context.Context, provider.SteerRequest) (provider.DeliveryReceipt, error) {
	return provider.DeliveryReceipt{}, nil
}

func (*fakeSession) RespondPermission(context.Context, provider.PermissionResponse) (provider.DeliveryReceipt, error) {
	return provider.DeliveryReceipt{}, nil
}

func (*fakeSession) AnswerQuestion(context.Context, provider.QuestionAnswer) (provider.DeliveryReceipt, error) {
	return provider.DeliveryReceipt{}, nil
}

func (*fakeSession) InterruptTurn(context.Context, provider.InterruptRequest) (provider.InterruptResult, error) {
	return provider.InterruptResult{}, nil
}

func (*fakeSession) Close(context.Context, provider.CloseSessionRequest) (provider.CloseSessionResult, error) {
	return provider.CloseSessionResult{}, nil
}

type fakeEventStream struct{}

func (fakeEventStream) Receive(context.Context) (provider.ProviderEvent, error) {
	return provider.ProviderEvent{}, nil
}

func (fakeEventStream) Close() error { return nil }

var (
	_ provider.Provider          = (*fakeProvider)(nil)
	_ provider.ResumableProvider = (*fakeProvider)(nil)
	_ provider.Session           = (*fakeSession)(nil)
	_ provider.EventStream       = fakeEventStream{}
)

func ExampleProvider() {
	p := &fakeProvider{session: &fakeSession{}}
	opened, _ := p.OpenSession(context.Background(), provider.OpenSessionRequest{})
	_ = opened.Session.Ref()
	// Output:
}
