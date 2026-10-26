package chatstore

import (
	"database/sql"

	"github.com/Pauwelz/whisker/daemon/internal/whatsapp"
)

// goSteps are Go steps that run in the same transaction as a migration file, after its SQL.
var goSteps = map[string]func(*sql.Tx) error{
	"0024_clean_names.sql": cleanNames,
}

// cleanNames cleans the names that the store already holds, with the caps of whatsapp.CleanName.
// A name is rewritten only when cleaning changes it, so a second run changes nothing.
func cleanNames(tx *sql.Tx) error {
	for _, c := range []struct {
		table, column string
		max           int
		where         string
	}{
		{"names", "push_name", whatsapp.MaxPushName, ""},
		{"names", "contact_name", 0, ""},
		{"chats", "subject", whatsapp.MaxGroupSubject, ""},
		{"media", "name", 0, ""},
		{"messages", "system_value", whatsapp.MaxGroupSubject, " AND system_kind = 'subject'"},
	} {
		if err := cleanColumn(tx, c.table, c.column, c.max, c.where); err != nil {
			return err
		}
	}
	return nil
}

// cleanColumn identifiers are fixed strings from cleanNames, never user input.
func cleanColumn(tx *sql.Tx, table, column string, max int, where string) error {
	rows, err := tx.Query("SELECT rowid, " + column + " FROM " + table + " WHERE " + column + " <> ''" + where)
	if err != nil {
		return err
	}
	type fix struct {
		rowid int64
		value string
	}
	var fixes []fix
	for rows.Next() {
		var id int64
		var v string
		if err := rows.Scan(&id, &v); err != nil {
			rows.Close()
			return err
		}
		if c := whatsapp.CleanName(v, max); c != v {
			fixes = append(fixes, fix{id, c})
		}
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, f := range fixes {
		if _, err := tx.Exec("UPDATE "+table+" SET "+column+" = ? WHERE rowid = ?", f.value, f.rowid); err != nil {
			return err
		}
	}
	return rows.Err()
}
