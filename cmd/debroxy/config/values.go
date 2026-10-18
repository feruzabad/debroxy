package config

import (
	"log/slog"
	"time"
)

// Duration is a time.Duration written as a Go duration string ("30s", "720h").
type Duration time.Duration

func (d Duration) Std() time.Duration { return time.Duration(d) }

// Secret holds a value read from the environment. Every way of printing it
// returns a redacted form (NFR-03, ddd-rules OUT-6); only Reveal returns the
// value.
type Secret struct{ value string }

func NewSecret(value string) Secret { return Secret{value: value} }

// Reveal returns the secret. Call it only where the value is used.
func (s Secret) Reveal() string { return s.value }
func (s Secret) IsZero() bool   { return s.value == "" }

func (s Secret) String() string               { return redacted }
func (s Secret) GoString() string             { return redacted }
func (s Secret) LogValue() slog.Value         { return slog.StringValue(redacted) }
func (s Secret) MarshalJSON() ([]byte, error) { return []byte(`"` + redacted + `"`), nil }
func (s Secret) MarshalText() ([]byte, error) { return []byte(redacted), nil }

const redacted = "<redacted>"
