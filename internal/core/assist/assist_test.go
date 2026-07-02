package assist

import (
	"reflect"
	"testing"
)

func TestPublishNoSubscribersReportsNotDelivered(t *testing.T) {
	b := NewBus()
	if b.HasSubscribers() {
		t.Fatal("fresh bus should have no subscribers")
	}
	if b.Publish(Event{Kind: KindHighlight, Target: TargetInputLine}) {
		t.Error("Publish with no subscribers should report delivered=false")
	}
}

func TestSubscribeReceivesEvent(t *testing.T) {
	b := NewBus()
	ch, unsub := b.Subscribe()
	defer unsub()
	if !b.HasSubscribers() {
		t.Fatal("expected a subscriber after Subscribe")
	}
	want := Event{Kind: KindPrefill, Target: TargetInputLine, Text: "select 1"}
	if !b.Publish(want) {
		t.Fatal("Publish with a subscriber should report delivered=true")
	}
	got := <-ch
	if !reflect.DeepEqual(got, want) {
		t.Errorf("received %+v, want %+v", got, want)
	}
}

func TestUnsubscribeStopsDelivery(t *testing.T) {
	b := NewBus()
	ch, unsub := b.Subscribe()
	unsub()
	if b.HasSubscribers() {
		t.Error("unsubscribe should remove the subscriber")
	}
	if _, ok := <-ch; ok {
		t.Error("channel should be closed after unsubscribe")
	}
	if b.Publish(Event{Kind: KindFocus, Target: TargetGrid}) {
		t.Error("Publish after unsubscribe should report delivered=false")
	}
}

func TestFanOutToMultipleSubscribers(t *testing.T) {
	b := NewBus()
	ch1, unsub1 := b.Subscribe()
	ch2, unsub2 := b.Subscribe()
	defer unsub1()
	defer unsub2()
	e := Event{Kind: KindAnnotate, Target: TargetEditor, Text: "here"}
	b.Publish(e)
	if got := <-ch1; !reflect.DeepEqual(got, e) {
		t.Errorf("sub1 got %+v", got)
	}
	if got := <-ch2; !reflect.DeepEqual(got, e) {
		t.Errorf("sub2 got %+v", got)
	}
}

func TestPublishIsNonBlockingWhenBufferFull(t *testing.T) {
	b := NewBus()
	_, unsub := b.Subscribe() // never drained
	defer unsub()
	// Far more than subBuffer; must not block or panic — excess is dropped.
	for i := 0; i < subBuffer*4; i++ {
		if !b.Publish(Event{Kind: KindHighlight, Target: TargetInputLine}) {
			t.Fatal("delivered should stay true while a subscriber exists")
		}
	}
}
