package secret

import "database/sql/driver"

type String string

const redacted = "REDACTED"

// String implements fmt.Stringer and redacts the sensitive value.
func (s String) String() string {
	return redacted
}

// GoString implements fmt.GoStringer and redacts the sensitive value.
func (s String) GoString() string {
	return redacted
}

// Raw returns the sensitive value as a string.
func (s String) Raw() string {
	return string(s)
}

// Value returns the sensitive value as a string for database (https://github.com/jackc/pgx) use.
//
// Deprecated: this exists for automatic database integration with secret values. Use
// Raw instead for general purpose secret handling.
func (s String) Value() (driver.Value, error) {
	return string(s), nil
}

func (s String) MarshalJSON() ([]byte, error) {
	return []byte(`"` + redacted + `"`), nil
}
