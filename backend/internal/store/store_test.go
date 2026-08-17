package store

import (
	"testing"

	"workbench/internal/model"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestCreateAndGet(t *testing.T) {
	s := newTestStore(t)
	tk, err := s.CreateTicket("退款", "要退款 800 元")
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.GetTicket(tk.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "退款" {
		t.Errorf("title = %q", got.Title)
	}
	if got.Status != model.StatusPending {
		t.Errorf("status = %q, want pending", got.Status)
	}
}

func TestSoftDelete(t *testing.T) {
	s := newTestStore(t)
	tk, _ := s.CreateTicket("x", "y")
	if err := s.SoftDelete(tk.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetTicket(tk.ID); err != ErrDeleted {
		t.Errorf("err = %v, want ErrDeleted", err)
	}
	if len(s.ListTickets()) != 0 {
		t.Error("deleted ticket should not appear in list")
	}
}

func TestEnqueueFrontReorder(t *testing.T) {
	s := newTestStore(t)
	a, _ := s.CreateTicket("a", "1")
	b, _ := s.CreateTicket("b", "2")
	// 初始队列 [a, b]
	if err := s.EnqueueFront(b.ID); err != nil {
		t.Fatal(err)
	}
	id, err := s.PopFront()
	if err != nil {
		t.Fatal(err)
	}
	if id != b.ID {
		t.Errorf("front = %q, want %q (重做插队)", id, b.ID)
	}
	id, _ = s.PopFront()
	if id != a.ID {
		t.Errorf("front = %q, want %q", id, a.ID)
	}
}

func TestConfirmAddsDataset(t *testing.T) {
	s := newTestStore(t)
	tk, _ := s.CreateTicket("退款", "要退款")
	a := model.Analysis{ID: "a1", TicketID: tk.ID, Category: "退款申请"}
	if err := s.AppendAnalysis(tk.ID, a); err != nil {
		t.Fatal(err)
	}
	err := s.ConfirmAnalysis(tk.ID, "a1", "退款申请", "高", "退款", "退款专员")
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Dataset()) != 1 {
		t.Fatalf("dataset size = %d, want 1", len(s.Dataset()))
	}
	if s.Dataset()[0].Category != "退款申请" {
		t.Errorf("dataset category = %q", s.Dataset()[0].Category)
	}
}

func TestActionShortcutConflict(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.AddAction("a", "指令1", "1"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddAction("b", "指令2", "1"); err != ErrActionExists {
		t.Errorf("err = %v, want ErrActionExists", err)
	}
}
