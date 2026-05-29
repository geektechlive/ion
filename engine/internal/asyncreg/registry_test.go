package asyncreg

import (
	"errors"
	"sync"
	"testing"
	"time"
)

// fakeDecl is a minimal Declaration used in the unit tests so the
// registry's behavior can be exercised without dragging the
// extension package's heavier types in here.
type fakeDecl struct {
	id   string
	data string
}

func (f *fakeDecl) ID() string { return f.id }

func TestRegister_HappyPath(t *testing.T) {
	r := New(0) // default cap
	decl := &fakeDecl{id: "/hooks/a"}

	if err := r.Register(KindWebhook, decl, OriginInit, nil); err != nil {
		t.Fatalf("Register failed: %v", err)
	}
	got, ok := r.ByID(KindWebhook, "/hooks/a")
	if !ok {
		t.Fatal("ByID did not return registered declaration")
	}
	if got.ID() != "/hooks/a" {
		t.Fatalf("ByID returned wrong id: %q", got.ID())
	}
	if c := r.Count(KindWebhook); c != 1 {
		t.Fatalf("Count after register = %d, want 1", c)
	}
	// Different kind buckets are independent.
	if c := r.Count(KindSchedule); c != 0 {
		t.Fatalf("schedule count leaked from webhook register: %d", c)
	}
}

func TestRegister_RejectsEmptyID(t *testing.T) {
	r := New(0)
	if err := r.Register(KindWebhook, &fakeDecl{}, OriginInit, nil); !errors.Is(err, ErrEmptyID) {
		t.Fatalf("empty id should be ErrEmptyID, got %v", err)
	}
}

func TestRegister_Duplicate(t *testing.T) {
	r := New(0)
	d := &fakeDecl{id: "x"}
	if err := r.Register(KindWebhook, d, OriginInit, nil); err != nil {
		t.Fatalf("first register: %v", err)
	}
	if err := r.Register(KindWebhook, d, OriginRuntime, nil); !errors.Is(err, ErrDuplicate) {
		t.Fatalf("duplicate register: want ErrDuplicate, got %v", err)
	}
}

func TestRegister_CapEnforced(t *testing.T) {
	r := New(2)
	for i, id := range []string{"a", "b"} {
		if err := r.Register(KindWebhook, &fakeDecl{id: id}, OriginInit, nil); err != nil {
			t.Fatalf("Register %d (%s) failed: %v", i, id, err)
		}
	}
	if err := r.Register(KindWebhook, &fakeDecl{id: "c"}, OriginRuntime, nil); !errors.Is(err, ErrCapExceeded) {
		t.Fatalf("cap should be enforced, got %v", err)
	}
	// Different kind has its own cap counter.
	if err := r.Register(KindSchedule, &fakeDecl{id: "s1"}, OriginInit, nil); err != nil {
		t.Fatalf("schedule register hit a webhook cap leak: %v", err)
	}
}

func TestRegister_VetoRollsBack(t *testing.T) {
	r := New(0)
	vetoCalled := 0
	veto := func(_ Kind, _ Declaration, _ Origin) error {
		vetoCalled++
		return errors.New("policy: blocked by test")
	}
	if err := r.Register(KindWebhook, &fakeDecl{id: "x"}, OriginRuntime, veto); err == nil {
		t.Fatal("expected veto error, got nil")
	}
	if vetoCalled != 1 {
		t.Fatalf("veto called %d times, want 1", vetoCalled)
	}
	if c := r.Count(KindWebhook); c != 0 {
		t.Fatalf("veto did not roll back count: %d", c)
	}
}

func TestRegister_VetoCanInspectDeclaration(t *testing.T) {
	r := New(0)
	var seen Declaration
	veto := func(_ Kind, d Declaration, _ Origin) error {
		seen = d
		// Allow this one.
		return nil
	}
	decl := &fakeDecl{id: "ok", data: "payload"}
	if err := r.Register(KindWebhook, decl, OriginInit, veto); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if seen == nil || seen.ID() != "ok" {
		t.Fatalf("veto did not see declaration, got %v", seen)
	}
}

func TestDeregister_FiresNotify(t *testing.T) {
	r := New(0)
	if err := r.Register(KindSchedule, &fakeDecl{id: "job-1"}, OriginInit, nil); err != nil {
		t.Fatalf("Register: %v", err)
	}
	notifyCalls := 0
	var seenKind Kind
	var seenOrigin Origin
	ok := r.Deregister(KindSchedule, "job-1", func(k Kind, _ Declaration, o Origin) {
		notifyCalls++
		seenKind = k
		seenOrigin = o
	})
	if !ok {
		t.Fatal("Deregister returned false for existing id")
	}
	if notifyCalls != 1 {
		t.Fatalf("notify called %d times, want 1", notifyCalls)
	}
	if seenKind != KindSchedule || seenOrigin != OriginInit {
		t.Fatalf("notify got kind=%s origin=%s, want schedule/init", seenKind, seenOrigin)
	}
	if c := r.Count(KindSchedule); c != 0 {
		t.Fatalf("Count after deregister = %d, want 0", c)
	}
}

func TestDeregister_UnknownIDIsNoop(t *testing.T) {
	r := New(0)
	notifyCalls := 0
	ok := r.Deregister(KindWebhook, "nope", func(_ Kind, _ Declaration, _ Origin) {
		notifyCalls++
	})
	if ok {
		t.Fatal("Deregister should return false for unknown id")
	}
	if notifyCalls != 0 {
		t.Fatalf("notify called %d times for unknown id, want 0", notifyCalls)
	}
}

func TestSubscribe_PublishesAddAndRemove(t *testing.T) {
	r := New(0)
	ch, cancel := r.Subscribe(KindWebhook, 4)
	defer cancel()

	if err := r.Register(KindWebhook, &fakeDecl{id: "a"}, OriginInit, nil); err != nil {
		t.Fatalf("Register: %v", err)
	}

	select {
	case ev := <-ch:
		if ev.Op != ChangeAdded || ev.ID != "a" || ev.Origin != OriginInit || ev.Kind != KindWebhook {
			t.Fatalf("unexpected add event: %+v", ev)
		}
	case <-time.After(time.Second):
		t.Fatal("did not receive add event in time")
	}

	r.Deregister(KindWebhook, "a", nil)
	select {
	case ev := <-ch:
		if ev.Op != ChangeRemoved || ev.ID != "a" {
			t.Fatalf("unexpected remove event: %+v", ev)
		}
	case <-time.After(time.Second):
		t.Fatal("did not receive remove event in time")
	}
}

func TestSubscribe_IgnoresOtherKind(t *testing.T) {
	r := New(0)
	ch, cancel := r.Subscribe(KindWebhook, 4)
	defer cancel()

	if err := r.Register(KindSchedule, &fakeDecl{id: "s1"}, OriginInit, nil); err != nil {
		t.Fatalf("Register: %v", err)
	}

	select {
	case ev := <-ch:
		t.Fatalf("unexpected event on webhook subscriber: %+v", ev)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestSubscribe_CancelCloses(t *testing.T) {
	r := New(0)
	ch, cancel := r.Subscribe(KindWebhook, 1)
	cancel()
	select {
	case _, ok := <-ch:
		if ok {
			t.Fatal("expected channel to be closed after cancel")
		}
	case <-time.After(time.Second):
		t.Fatal("channel did not close after cancel")
	}
}

func TestReentrantRegisterFromVeto(t *testing.T) {
	// The veto for /a registers /b on its way through. Since the veto
	// runs outside the registry mutex, the inner Register must succeed
	// and the outer must commit normally.
	r := New(0)
	var nested error
	veto := func(_ Kind, _ Declaration, _ Origin) error {
		nested = r.Register(KindWebhook, &fakeDecl{id: "/b"}, OriginRuntime, nil)
		return nil
	}
	if err := r.Register(KindWebhook, &fakeDecl{id: "/a"}, OriginInit, veto); err != nil {
		t.Fatalf("outer register failed: %v", err)
	}
	if nested != nil {
		t.Fatalf("nested register failed inside veto: %v", nested)
	}
	if c := r.Count(KindWebhook); c != 2 {
		t.Fatalf("expected 2 entries after reentrant register, got %d", c)
	}
}

func TestConcurrentRegisters(t *testing.T) {
	// Race-test the duplicate-detection / cap enforcement under
	// concurrent load. Goroutines try to register colliding ids; only
	// one must win per id, and the cap must never be exceeded.
	r := New(50)
	var wg sync.WaitGroup
	var dupErrs, capErrs, okCount int
	var mu sync.Mutex
	for i := 0; i < 200; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			id := "k" + itoa(i%30) // 30 distinct ids contended by 200 goroutines
			err := r.Register(KindWebhook, &fakeDecl{id: id}, OriginRuntime, nil)
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				okCount++
			case errors.Is(err, ErrDuplicate):
				dupErrs++
			case errors.Is(err, ErrCapExceeded):
				capErrs++
			default:
				t.Errorf("unexpected error: %v", err)
			}
		}()
	}
	wg.Wait()
	if okCount > 30 {
		t.Fatalf("more successful registers than distinct ids: ok=%d", okCount)
	}
	if r.Count(KindWebhook) != okCount {
		t.Fatalf("count=%d != okCount=%d", r.Count(KindWebhook), okCount)
	}
	if dupErrs+okCount+capErrs != 200 {
		t.Fatalf("missing results: ok=%d dup=%d cap=%d total=200", okCount, dupErrs, capErrs)
	}
}

// itoa is a tiny dependency-free helper. Avoids strconv just for tests.
func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	neg := i < 0
	if neg {
		i = -i
	}
	var b [10]byte
	pos := len(b)
	for i > 0 {
		pos--
		b[pos] = byte('0' + i%10)
		i /= 10
	}
	if neg {
		pos--
		b[pos] = '-'
	}
	return string(b[pos:])
}
