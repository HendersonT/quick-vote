package clock

import (
	"testing"
	"time"
)

func TestFakeAdvanceFiresDueTimersInOrder(t *testing.T) {
	c := NewFake(time.Unix(1000, 0))
	var got []string
	c.AfterFunc(2*time.Second, func() { got = append(got, "b") })
	c.AfterFunc(1*time.Second, func() { got = append(got, "a") })
	late := c.AfterFunc(5*time.Second, func() { got = append(got, "late") })

	c.Advance(3 * time.Second)
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("fired %v, want [a b]", got)
	}
	if !late.Stop() {
		t.Fatal("Stop on a pending timer should report true")
	}
	c.Advance(10 * time.Second)
	if len(got) != 2 {
		t.Fatalf("stopped timer fired: %v", got)
	}
	if want := time.Unix(1013, 0); !c.Now().Equal(want) {
		t.Fatalf("Now = %v, want %v", c.Now(), want)
	}
}

func TestFakeZeroDelayFiresOnNextAdvance(t *testing.T) {
	c := NewFake(time.Unix(0, 0))
	fired := false
	c.AfterFunc(0, func() { fired = true })
	c.Advance(0)
	if !fired {
		t.Fatal("zero-delay timer should fire on Advance(0)")
	}
}

func TestFakeCallbackMaySchedule(t *testing.T) {
	c := NewFake(time.Unix(0, 0))
	n := 0
	c.AfterFunc(time.Second, func() {
		n++
		c.AfterFunc(time.Second, func() { n++ })
	})
	c.Advance(2 * time.Second)
	if n != 2 {
		t.Fatalf("n = %d, want 2 (timer scheduled inside a callback and due within the advance window fires)", n)
	}
}
