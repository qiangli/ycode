package prompt

import "testing"

func TestTopicTracker_ExplicitMarker(t *testing.T) {
	tt := NewTopicTracker()
	tt.Update("Let's work on the authentication middleware")

	topic := tt.CurrentTopic()
	if topic != "the authentication middleware" {
		t.Errorf("unexpected topic: %q", topic)
	}

	inject := tt.Inject()
	if inject != "[Active Topic: the authentication middleware]" {
		t.Errorf("unexpected inject: %q", inject)
	}
}

func TestTopicTracker_ShortDirective(t *testing.T) {
	tt := NewTopicTracker()
	tt.Update("fix the login bug")

	topic := tt.CurrentTopic()
	if topic != "fix the login bug" {
		t.Errorf("unexpected topic: %q", topic)
	}
}

func TestTopicTracker_FollowUpDoesNotChangeTopic(t *testing.T) {
	tt := NewTopicTracker()
	tt.Update("implement the API endpoint")
	tt.Update("yes that looks good")

	topic := tt.CurrentTopic()
	if topic != "implement the API endpoint" {
		t.Errorf("expected topic unchanged, got: %q", topic)
	}
}

func TestTopicTracker_StaleClear(t *testing.T) {
	tt := NewTopicTracker()
	tt.Update("fix the bug")

	// Simulate 21 turns of follow-ups.
	for i := 0; i < TopicMaxTurns+1; i++ {
		tt.Update("yes")
	}

	if tt.CurrentTopic() != "" {
		t.Error("expected topic cleared after max turns")
	}
	if tt.Inject() != "" {
		t.Error("expected empty inject after stale")
	}
}

func TestTopicTracker_SetTopic(t *testing.T) {
	tt := NewTopicTracker()
	tt.SetTopic("restored from snapshot")

	if tt.CurrentTopic() != "restored from snapshot" {
		t.Errorf("unexpected topic: %q", tt.CurrentTopic())
	}
}

func TestTopicTracker_EmptyInject(t *testing.T) {
	tt := NewTopicTracker()
	if tt.Inject() != "" {
		t.Error("expected empty inject with no topic")
	}
}

func TestTopicTracker_SentenceExtraction(t *testing.T) {
	tt := NewTopicTracker()
	tt.Update("Please fix the auth bug. Also check the tests.")

	topic := tt.CurrentTopic()
	if topic != "the auth bug" {
		t.Errorf("expected first sentence topic, got: %q", topic)
	}
}

func TestTopicTracker_HelpMeWith(t *testing.T) {
	tt := NewTopicTracker()
	tt.Update("help me with refactoring the database layer")

	if tt.CurrentTopic() != "refactoring the database layer" {
		t.Errorf("unexpected topic: %q", tt.CurrentTopic())
	}
}
