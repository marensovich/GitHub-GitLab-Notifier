package storage

import (
	"slices"
	"testing"
)

func gh(id, login string) Account {
	return Account{Platform: "github", Host: "github.com", AccountID: id, Login: login}
}

func TestLinkReplacesOtherOwner(t *testing.T) {
	s := openTest(t)
	a, b := dest(t, s, "telegram", "1"), dest(t, s, "telegram", "2")
	if replaced, err := s.Link("telegram", "1", a.ID, gh("10", "alice")); err != nil || replaced {
		t.Fatalf("first link: %v %v", replaced, err)
	}
	if replaced, err := s.Link("telegram", "2", b.ID, gh("10", "alice")); err != nil || !replaced {
		t.Fatalf("second link: %v %v", replaced, err)
	}
	if _, accs, _ := s.PersonOf("telegram", "1"); len(accs) != 0 {
		t.Errorf("old owner still has %+v", accs)
	}
	if _, accs, _ := s.PersonOf("telegram", "2"); len(accs) != 1 || accs[0].Login != "alice" {
		t.Errorf("new owner has %+v", accs)
	}
	// The same account in another messenger is a separate link.
	c := dest(t, s, "discord", "dm1")
	if replaced, err := s.Link("discord", "77", c.ID, gh("10", "alice")); err != nil || replaced {
		t.Fatalf("discord link: %v %v", replaced, err)
	}
	if _, accs, _ := s.PersonOf("telegram", "2"); len(accs) != 1 {
		t.Errorf("telegram link lost: %+v", accs)
	}
}

func TestLinkKeepsMutedAndUpdatesLogin(t *testing.T) {
	s := openTest(t)
	d := dest(t, s, "telegram", "1")
	if _, err := s.Link("telegram", "1", d.ID, gh("10", "alice")); err != nil {
		t.Fatal(err)
	}
	p, _, _ := s.PersonOf("telegram", "1")
	if err := s.SetMuted(p.ID, []string{"mention"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Link("telegram", "1", d.ID, gh("10", "alice2")); err != nil {
		t.Fatal(err)
	}
	p, accs, err := s.PersonOf("telegram", "1")
	if err != nil || !slices.Equal(p.Muted, []string{"mention"}) || len(accs) != 1 || accs[0].Login != "alice2" {
		t.Errorf("person %+v accounts %+v err %v", p, accs, err)
	}
	if none, _, err := s.PersonOf("telegram", "9"); none != nil || err != nil {
		t.Errorf("unknown person: %+v %v", none, err)
	}
}

func TestRecipientsByIDAndLogin(t *testing.T) {
	s := openTest(t)
	a, b, c := dest(t, s, "telegram", "1"), dest(t, s, "telegram", "2"), dest(t, s, "telegram", "3")
	s.Link("telegram", "1", a.ID, gh("10", "Alice"))
	s.Link("telegram", "2", b.ID, gh("20", "bob"))
	s.Link("telegram", "3", c.ID, gh("30", "carol"))
	s.Link("telegram", "1", a.ID, Account{Platform: "gitlab", Host: "gitlab.com", AccountID: "10", Login: "alice"})
	s.SetDestinationInactive(c.ID, true)

	got, err := s.Recipients("github", "github.com", []string{"20", "30"}, []string{"alice", "carol"})
	if err != nil {
		t.Fatal(err)
	}
	var logins []string
	for _, r := range got {
		logins = append(logins, r.Login+"@"+r.UserID)
	}
	slices.Sort(logins)
	if !slices.Equal(logins, []string{"Alice@1", "bob@2"}) {
		t.Errorf("recipients %v", logins)
	}
	if got, _ := s.Recipients("github", "github.com", nil, nil); len(got) != 0 {
		t.Errorf("empty query: %+v", got)
	}
}

func TestUnlinkByLoginAndAll(t *testing.T) {
	s := openTest(t)
	a := dest(t, s, "telegram", "1")
	s.Link("telegram", "1", a.ID, gh("10", "alice"))
	s.Link("telegram", "1", a.ID, gh("11", "alice-work"))
	s.Link("telegram", "1", a.ID, Account{Platform: "gitlab", Host: "gitlab.com", AccountID: "5", Login: "alice"})
	if n, err := s.Unlink("telegram", "1", "github", "ALICE"); n != 1 || err != nil {
		t.Errorf("unlink one: %d %v", n, err)
	}
	if n, err := s.Unlink("telegram", "1", "github", ""); n != 1 || err != nil {
		t.Errorf("unlink all github: %d %v", n, err)
	}
	b := dest(t, s, "discord", "dm")
	s.Link("discord", "7", b.ID, Account{Platform: "gitlab", Host: "gitlab.com", AccountID: "5", Login: "alice"})
	if n, err := s.UnlinkLogin("gitlab", "alice"); n != 2 || err != nil {
		t.Errorf("admin unlink: %d %v", n, err)
	}
	if links, _ := s.Links(); len(links) != 0 {
		t.Errorf("left %+v", links)
	}
}

func TestPersonDeletedWithDestination(t *testing.T) {
	s := openTest(t)
	a := dest(t, s, "telegram", "1")
	s.Link("telegram", "1", a.ID, gh("10", "alice"))
	if _, err := s.db.Exec("DELETE FROM destinations WHERE id = ?", a.ID); err != nil {
		t.Fatal(err)
	}
	if p, accs, _ := s.PersonOf("telegram", "1"); p != nil || len(accs) != 0 {
		t.Errorf("left %+v %+v", p, accs)
	}
}

func TestLinksListsEveryone(t *testing.T) {
	s := openTest(t)
	a, b := dest(t, s, "telegram", "1"), dest(t, s, "discord", "dm")
	s.Link("telegram", "1", a.ID, gh("10", "alice"))
	s.Link("discord", "7", b.ID, gh("20", "bob"))
	links, err := s.Links()
	if err != nil || len(links) != 2 || links[0].Messenger != "discord" || links[1].Login != "alice" {
		t.Errorf("links %+v %v", links, err)
	}
}

func TestMigrationFromV5(t *testing.T) {
	s := mustOpen(t, openAtVersion(t, 5))
	if v, err := s.SchemaVersion(); err != nil || v < 6 {
		t.Fatalf("version %d %v", v, err)
	}
	d := dest(t, s, "telegram", "1")
	if _, err := s.Link("telegram", "1", d.ID, gh("10", "alice")); err != nil {
		t.Error(err)
	}
}
