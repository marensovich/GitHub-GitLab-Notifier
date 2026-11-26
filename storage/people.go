package storage

import (
	"database/sql"
	"errors"
	"strings"
)

// Account is a GitHub or GitLab account a person proved is theirs.
type Account struct {
	// Platform is "github" or "gitlab"; Host is lower-case.
	Platform, Host, AccountID, Login string
}

// Person is a messenger user who linked an account, with their DM.
type Person struct {
	ID            int64
	Messenger     string
	UserID        string
	DestinationID int64
	// Muted lists the reasons the person gets no DM for.
	Muted []string
}

// Recipient is one linked account with its person.
type Recipient struct {
	Person
	Account
}

func splitMuted(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, ",")
}

// Link ties an account to the messenger user, whose DM is destID. A link of
// the same account in the same messenger owned by someone else is removed;
// replaced reports that.
func (s *Store) Link(messenger, userID string, destID int64, a Account) (replaced bool, err error) {
	tx, err := s.db.Begin()
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`INSERT INTO people (messenger, user_id, destination_id) VALUES (?, ?, ?)
		ON CONFLICT (messenger, user_id) DO UPDATE SET destination_id = excluded.destination_id`,
		messenger, userID, destID); err != nil {
		return false, err
	}
	var personID int64
	if err := tx.QueryRow("SELECT id FROM people WHERE messenger = ? AND user_id = ?", messenger, userID).Scan(&personID); err != nil {
		return false, err
	}
	res, err := tx.Exec(`DELETE FROM accounts WHERE messenger = ? AND platform = ? AND host = ? AND account_id = ? AND person_id != ?`,
		messenger, a.Platform, a.Host, a.AccountID, personID)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	if _, err := tx.Exec(`INSERT INTO accounts (person_id, messenger, platform, host, account_id, login) VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT (messenger, platform, host, account_id) DO UPDATE SET login = excluded.login, linked_at = CURRENT_TIMESTAMP`,
		personID, messenger, a.Platform, a.Host, a.AccountID, a.Login); err != nil {
		return false, err
	}
	return n > 0, tx.Commit()
}

// PersonOf returns the person of a messenger user with their accounts, or
// a nil person when they linked nothing.
func (s *Store) PersonOf(messenger, userID string) (*Person, []Account, error) {
	var p Person
	var muted string
	err := s.db.QueryRow("SELECT id, messenger, user_id, destination_id, muted FROM people WHERE messenger = ? AND user_id = ?",
		messenger, userID).Scan(&p.ID, &p.Messenger, &p.UserID, &p.DestinationID, &muted)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	p.Muted = splitMuted(muted)
	rows, err := s.db.Query("SELECT platform, host, account_id, login FROM accounts WHERE person_id = ? ORDER BY platform, host, login", p.ID)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	var accs []Account
	for rows.Next() {
		var a Account
		if err := rows.Scan(&a.Platform, &a.Host, &a.AccountID, &a.Login); err != nil {
			return nil, nil, err
		}
		accs = append(accs, a)
	}
	return &p, accs, rows.Err()
}

// Unlink removes the user's links on a platform: the one with this login,
// or all of them when login is "".
func (s *Store) Unlink(messenger, userID, platform, login string) (int, error) {
	q := `DELETE FROM accounts WHERE platform = ?
		AND person_id = (SELECT id FROM people WHERE messenger = ? AND user_id = ?)`
	args := []any{platform, messenger, userID}
	if login != "" {
		q += " AND login = ?"
		args = append(args, login)
	}
	return s.affected(q, args...)
}

// UnlinkLogin removes every link of a login on a platform, in every
// messenger (for bot administrators).
func (s *Store) UnlinkLogin(platform, login string) (int, error) {
	return s.affected("DELETE FROM accounts WHERE platform = ? AND login = ?", platform, login)
}

func (s *Store) affected(q string, args ...any) (int, error) {
	res, err := s.db.Exec(q, args...)
	if err != nil {
		return 0, err
	}
	n, err := res.RowsAffected()
	return int(n), err
}

// RenameAccount stores a new login of an account, known by its ID, in
// every messenger. It reports how many links changed.
func (s *Store) RenameAccount(platform, host, accountID, login string) (int, error) {
	return s.affected(`UPDATE accounts SET login = ?
		WHERE platform = ? AND host = ? AND account_id = ? AND login != ?`, login, platform, host, accountID, login)
}

// SetMuted sets the reasons a person gets no DM for.
func (s *Store) SetMuted(personID int64, muted []string) error {
	_, err := s.db.Exec("UPDATE people SET muted = ? WHERE id = ?", strings.Join(muted, ","), personID)
	return err
}

const recipientColumns = `p.id, p.messenger, p.user_id, p.destination_id, p.muted, a.platform, a.host, a.account_id, a.login`

func (s *Store) queryRecipients(q string, args ...any) ([]Recipient, error) {
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []Recipient
	for rows.Next() {
		var r Recipient
		var muted string
		if err := rows.Scan(&r.Person.ID, &r.Messenger, &r.UserID, &r.DestinationID, &muted,
			&r.Platform, &r.Host, &r.AccountID, &r.Login); err != nil {
			return nil, err
		}
		r.Muted = splitMuted(muted)
		list = append(list, r)
	}
	return list, rows.Err()
}

// Recipients returns the linked accounts on a platform host whose ID is in
// ids or whose login (any case) is in logins, for people whose DM is
// active.
func (s *Store) Recipients(platform, host string, ids, logins []string) ([]Recipient, error) {
	if len(ids) == 0 && len(logins) == 0 {
		return nil, nil
	}
	var match []string
	args := []any{platform, host}
	if len(ids) > 0 {
		match = append(match, "a.account_id IN ("+placeholders(len(ids))+")")
		for _, id := range ids {
			args = append(args, id)
		}
	}
	if len(logins) > 0 {
		match = append(match, "a.login IN ("+placeholders(len(logins))+")")
		for _, l := range logins {
			args = append(args, l)
		}
	}
	return s.queryRecipients(`SELECT `+recipientColumns+`
		FROM accounts a JOIN people p ON p.id = a.person_id JOIN destinations d ON d.id = p.destination_id
		WHERE a.platform = ? AND a.host = ? AND d.inactive = 0 AND (`+strings.Join(match, " OR ")+`)
		ORDER BY p.id, a.id`, args...)
}

// Links returns every linked account, by messenger, platform and login.
func (s *Store) Links() ([]Recipient, error) {
	return s.queryRecipients(`SELECT ` + recipientColumns + `
		FROM accounts a JOIN people p ON p.id = a.person_id
		ORDER BY p.messenger, a.platform, a.login`)
}

func placeholders(n int) string {
	return strings.TrimSuffix(strings.Repeat("?, ", n), ", ")
}
