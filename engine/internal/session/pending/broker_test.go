package pending

import (
	"sync"
	"testing"
	"time"
)

func TestBroker_PermissionRoundTrip(t *testing.T) {
	b := New()
	ch := b.RegisterPermission("q1")

	go func() {
		time.Sleep(time.Millisecond)
		b.ResolvePermission("q1", "allow")
	}()

	select {
	case v := <-ch:
		if v != "allow" {
			t.Errorf("expected 'allow', got %q", v)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout")
	}

	b.UnregisterPermission("q1")
}

func TestBroker_DialogRoundTrip(t *testing.T) {
	b := New()
	ch := b.RegisterDialog("d1")

	go func() {
		time.Sleep(time.Millisecond)
		b.ResolveDialog("d1", "yes")
	}()

	select {
	case v := <-ch:
		if v != "yes" {
			t.Errorf("expected 'yes', got %v", v)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout")
	}
}

func TestBroker_ElicitRoundTrip(t *testing.T) {
	b := New()
	ch := b.RegisterElicit("e1")

	go func() {
		time.Sleep(time.Millisecond)
		b.ResolveElicit("e1", ElicitReply{
			Response: map[string]interface{}{"answer": "42"},
		})
	}()

	select {
	case v := <-ch:
		if v.Response["answer"] != "42" {
			t.Errorf("expected answer=42, got %v", v.Response)
		}
		if v.Cancelled {
			t.Error("expected not cancelled")
		}
	case <-time.After(time.Second):
		t.Fatal("timeout")
	}
}

func TestBroker_ElicitCancelled(t *testing.T) {
	b := New()
	ch := b.RegisterElicit("e2")

	go func() {
		time.Sleep(time.Millisecond)
		b.ResolveElicit("e2", ElicitReply{Cancelled: true})
	}()

	select {
	case v := <-ch:
		if !v.Cancelled {
			t.Error("expected cancelled")
		}
	case <-time.After(time.Second):
		t.Fatal("timeout")
	}
}

func TestBroker_ResolveUnknownIsNoop(t *testing.T) {
	b := New()
	// None of these should panic.
	b.ResolvePermission("nope", "x")
	b.ResolveDialog("nope", "x")
	b.ResolveElicit("nope", ElicitReply{})
}

func TestBroker_UnregisterRemovesEntry(t *testing.T) {
	b := New()
	b.RegisterPermission("p1")
	b.UnregisterPermission("p1")

	// Resolve should be a no-op after unregister.
	b.ResolvePermission("p1", "late")
}

func TestBroker_ConcurrentAccess(t *testing.T) {
	b := New()
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(3)
		go func(idx int) {
			defer wg.Done()
			id := "p" + string(rune('0'+idx%10))
			ch := b.RegisterPermission(id)
			go func() { b.ResolvePermission(id, "ok") }()
			select {
			case <-ch:
			case <-time.After(100 * time.Millisecond):
			}
			b.UnregisterPermission(id)
		}(i)
		go func(idx int) {
			defer wg.Done()
			id := "d" + string(rune('0'+idx%10))
			ch := b.RegisterDialog(id)
			go func() { b.ResolveDialog(id, "ok") }()
			select {
			case <-ch:
			case <-time.After(100 * time.Millisecond):
			}
			b.UnregisterDialog(id)
		}(i)
		go func(idx int) {
			defer wg.Done()
			id := "e" + string(rune('0'+idx%10))
			ch := b.RegisterElicit(id)
			go func() { b.ResolveElicit(id, ElicitReply{}) }()
			select {
			case <-ch:
			case <-time.After(100 * time.Millisecond):
			}
			b.UnregisterElicit(id)
		}(i)
	}
	wg.Wait()
}

func TestBroker_DialogResolveUnknownReturnsFalse(t *testing.T) {
	b := New()
	if b.ResolveDialog("missing", "x") {
		t.Error("expected false for unknown dialog")
	}
}

func TestBroker_ElicitResolveUnknownReturnsFalse(t *testing.T) {
	b := New()
	if b.ResolveElicit("missing", ElicitReply{}) {
		t.Error("expected false for unknown elicit")
	}
}
