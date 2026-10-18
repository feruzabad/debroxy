package config

import (
	"fmt"
	"io"
	"maps"
	"net"
	"os"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// Prefix starts every variable debroxy reads.
const Prefix = "DEBROXY"

// Variable names outside the tagged structs (spec §11).
const (
	providersVariable   = Prefix + "_PROVIDERS"
	signingKeysVariable = Prefix + "_SIGNING_KEYS"
	fileSuffix          = "_FILE"
	rateBudgetSegment   = "_RATE_BUDGET_"
)

// maxSecretFileBytes caps a secret read from a _FILE variable.
const maxSecretFileBytes = 64 << 10

// FromEnv builds the configuration from environ ("NAME=value" entries, as
// os.Environ returns them): defaults, then every DEBROXY_* variable, then
// validation. All problems are reported together.
func FromEnv(environ []string) (Config, error) {
	l := newLoader(environ)
	cfg := Default()
	l.walk(reflect.ValueOf(&cfg).Elem(), Prefix)
	cfg.Delivery.SigningKeys = l.signingKeys()
	cfg.Providers = l.providers()
	l.reportUnknown()

	v := &validator{problems: l.problems}
	validate(v, &cfg)
	if len(v.problems) > 0 {
		return Config{}, ValidationError{Problems: v.problems}
	}
	return cfg, nil
}

// Lookup returns the value of name in environ, as os.LookupEnv would.
func Lookup(environ []string, name string) (string, bool) {
	for _, entry := range slices.Backward(environ) {
		if key, value, ok := strings.Cut(entry, "="); ok && key == name {
			return value, true
		}
	}
	return "", false
}

type loader struct {
	values   map[string]string
	known    map[string]bool
	problems []string
}

func newLoader(environ []string) *loader {
	l := &loader{values: map[string]string{}, known: map[string]bool{}}
	for _, entry := range environ {
		if key, value, ok := strings.Cut(entry, "="); ok && strings.HasPrefix(key, Prefix+"_") {
			l.values[key] = value
		}
	}
	return l
}

func (l *loader) addf(name, format string, args ...any) {
	l.problems = append(l.problems, name+": "+fmt.Sprintf(format, args...))
}

// lookup marks name as known and returns its value.
func (l *loader) lookup(name string) (string, bool) {
	l.known[name] = true
	value, ok := l.values[name]
	return value, ok
}

var (
	durationType = reflect.TypeFor[Duration]()
	secretType   = reflect.TypeFor[Secret]()
)

// walk sets every tagged field of the struct value from its variable.
func (l *loader) walk(value reflect.Value, prefix string) {
	for i := range value.NumField() {
		field := value.Type().Field(i)
		segment := field.Tag.Get("env")
		if segment == "" {
			continue
		}
		name := prefix + "_" + segment
		target := value.Field(i)
		switch {
		case field.Type == secretType:
			target.Set(reflect.ValueOf(l.secret(name)))
		case field.Type.Kind() == reflect.Struct && field.Type != durationType:
			l.walk(target, name)
		default:
			if raw, ok := l.lookup(name); ok {
				if err := setValue(target, raw); err != nil {
					l.addf(name, "%v", err)
				}
			}
		}
	}
}

// secret reads name or name_FILE (spec §11); setting both is an error.
func (l *loader) secret(name string) Secret {
	direct, hasDirect := l.lookup(name)
	path, hasFile := l.lookup(name + fileSuffix)
	switch {
	case hasDirect && hasFile:
		l.addf(name, "set either %s or %s, not both", name, name+fileSuffix)
		return Secret{}
	case hasFile:
		content, err := readSecretFile(path)
		if err != nil {
			l.addf(name+fileSuffix, "%v", err)
			return Secret{}
		}
		return NewSecret(content)
	}
	return NewSecret(direct)
}

func readSecretFile(path string) (string, error) {
	file, err := os.Open(path) //nolint:gosec // the operator names the secret file
	if err != nil {
		return "", fmt.Errorf("read secret file: %w", err)
	}
	defer func() { _ = file.Close() }()
	content, err := io.ReadAll(io.LimitReader(file, maxSecretFileBytes+1))
	if err != nil {
		return "", fmt.Errorf("read secret file: %w", err)
	}
	if len(content) > maxSecretFileBytes {
		return "", fmt.Errorf("secret file is larger than %d bytes", maxSecretFileBytes)
	}
	return strings.TrimRight(string(content), "\r\n"), nil
}

// setValue parses raw into target by its type.
func setValue(target reflect.Value, raw string) error {
	switch target.Interface().(type) {
	case string:
		target.SetString(raw)
	case int, int64:
		n, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
		if err != nil {
			return fmt.Errorf("must be an integer, got %q", raw)
		}
		target.SetInt(n)
	case bool:
		b, err := parseBool(raw)
		if err != nil {
			return err
		}
		target.SetBool(b)
	case *bool:
		b, err := parseBool(raw)
		if err != nil {
			return err
		}
		target.Set(reflect.ValueOf(&b))
	case Duration:
		d, err := parseDuration(raw)
		if err != nil {
			return err
		}
		target.Set(reflect.ValueOf(d))
	case *Duration:
		d, err := parseDuration(raw)
		if err != nil {
			return err
		}
		target.Set(reflect.ValueOf(&d))
	case []string:
		target.Set(reflect.ValueOf(splitList(raw)))
	case []Duration:
		var durations []Duration
		for _, item := range splitList(raw) {
			d, err := parseDuration(item)
			if err != nil {
				return err
			}
			durations = append(durations, d)
		}
		target.Set(reflect.ValueOf(durations))
	default:
		panic(fmt.Sprintf("config: no parser for %s", target.Type()))
	}
	return nil
}

func parseBool(raw string) (bool, error) {
	switch strings.TrimSpace(raw) {
	case "true":
		return true, nil
	case "false":
		return false, nil
	}
	return false, fmt.Errorf("must be true or false, got %q", raw)
}

func parseDuration(raw string) (Duration, error) {
	d, err := time.ParseDuration(strings.TrimSpace(raw))
	if err != nil {
		return 0, fmt.Errorf("must be a duration like \"30s\", got %q", raw)
	}
	return Duration(d), nil
}

// splitList splits a comma-separated list; an empty value is an empty list.
func splitList(raw string) []string {
	items := []string{}
	for item := range strings.SplitSeq(raw, ",") {
		if item = strings.TrimSpace(item); item != "" {
			items = append(items, item)
		}
	}
	return items
}

// IDSegment is an ID in a variable name: upper case, "-" as "_".
func IDSegment(id string) string {
	return strings.ToUpper(strings.ReplaceAll(id, "-", "_"))
}

// SigningKeyVariable names the variable of a signing key's secret.
func SigningKeyVariable(id string) string { return Prefix + "_SIGNING_KEY_" + IDSegment(id) }

// ProviderPrefix starts the variables of one provider.
func ProviderPrefix(id string) string { return Prefix + "_PROVIDER_" + IDSegment(id) }

// RateBudgetSegment is a budget class in a variable name: uncachedCreate
// becomes UNCACHED_CREATE.
func RateBudgetSegment(class string) string {
	var b strings.Builder
	for i, r := range class {
		if unicode.IsUpper(r) && i > 0 {
			b.WriteByte('_')
		}
		b.WriteRune(unicode.ToUpper(r))
	}
	return b.String()
}

func (l *loader) signingKeys() []SigningKey {
	raw, _ := l.lookup(signingKeysVariable)
	var keys []SigningKey
	for _, id := range splitList(raw) {
		keys = append(keys, SigningKey{ID: id, Secret: l.secret(SigningKeyVariable(id))})
	}
	return keys
}

func (l *loader) providers() []Provider {
	raw, _ := l.lookup(providersVariable)
	var providers []Provider
	for _, id := range splitList(raw) {
		prefix := ProviderPrefix(id)
		kind, _ := l.lookup(prefix + "_KIND")
		p := providerDefaults(id, kind)
		l.walk(reflect.ValueOf(&p).Elem(), prefix)
		if p.RateBudgets == nil {
			p.RateBudgets = map[string]RateBudget{}
		}
		maps.Copy(p.RateBudgets, l.rateBudgets(prefix))
		providers = append(providers, p)
	}
	return providers
}

// rateBudgets reads every <prefix>_RATE_BUDGET_<CLASS>=<requests>/<duration>.
// Known classes get their camel-case name; others keep the variable's
// segment, so validation reports them as unknown for the kind.
func (l *loader) rateBudgets(prefix string) map[string]RateBudget {
	classes := map[string]string{}
	for _, names := range rateBudgetNames {
		for _, class := range names {
			classes[RateBudgetSegment(class)] = class
		}
	}
	budgets := map[string]RateBudget{}
	for name, raw := range l.values {
		segment, ok := strings.CutPrefix(name, prefix+rateBudgetSegment)
		if !ok {
			continue
		}
		l.known[name] = true
		class, known := classes[segment]
		if !known {
			class = segment
		}
		budget, err := parseRateBudget(raw)
		if err != nil {
			l.addf(name, "%v", err)
			continue
		}
		budgets[class] = budget
	}
	return budgets
}

func parseRateBudget(raw string) (RateBudget, error) {
	requests, per, ok := strings.Cut(strings.TrimSpace(raw), "/")
	n, err := strconv.Atoi(requests)
	if !ok || err != nil {
		return RateBudget{}, fmt.Errorf("must be <requests>/<duration> like \"60/1h\", got %q", raw)
	}
	d, err := parseDuration(per)
	if err != nil {
		return RateBudget{}, err
	}
	return RateBudget{Requests: n, Per: d}, nil
}

// reportUnknown reports every DEBROXY_* variable no field read: a typo,
// an old name, or an ID that is not listed (spec §11).
func (l *loader) reportUnknown() {
	var unknown []string
	for name := range l.values {
		if !l.known[name] {
			unknown = append(unknown, name)
		}
	}
	slices.Sort(unknown)
	for _, name := range unknown {
		l.addf(name, "unknown variable")
	}
}

// Variables lists every variable FromEnv reads for the given providers
// (ID → kind) and signing key IDs, without the _FILE forms.
func Variables(providers map[string]string, signingKeyIDs []string) []string {
	l := newLoader(nil)
	cfg := Default()
	l.walk(reflect.ValueOf(&cfg).Elem(), Prefix)
	names := []string{providersVariable, signingKeysVariable}
	for _, id := range signingKeyIDs {
		names = append(names, SigningKeyVariable(id))
	}
	for id, kind := range providers {
		var p Provider
		prefix := ProviderPrefix(id)
		l.walk(reflect.ValueOf(&p).Elem(), prefix)
		for _, class := range rateBudgetNames[kind] {
			names = append(names, prefix+rateBudgetSegment+RateBudgetSegment(class))
		}
	}
	for name := range l.known {
		if !strings.HasSuffix(name, fileSuffix) {
			names = append(names, name)
		}
	}
	slices.Sort(names)
	return slices.Compact(names)
}

// HealthcheckURL is the loopback /readyz URL for the listen address in
// environ (default ":8080"); healthcheck reads nothing else (spec §11).
func HealthcheckURL(environ []string) (string, error) {
	listen, ok := Lookup(environ, Prefix+"_SERVER_LISTEN")
	if !ok || listen == "" {
		listen = Default().Server.Listen
	}
	host, port, err := net.SplitHostPort(listen)
	if err != nil || port == "" {
		return "", fmt.Errorf("%s_SERVER_LISTEN: must be host:port or :port, got %q", Prefix, listen)
	}
	switch host {
	case "", "0.0.0.0":
		host = "127.0.0.1"
	case "::":
		host = "::1"
	}
	return "http://" + net.JoinHostPort(host, port) + "/readyz", nil
}
