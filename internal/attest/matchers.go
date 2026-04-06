package attest

import (
	"cmp"
	"fmt"
	"reflect"
	"regexp"
	"strings"

	"github.com/tidwall/gjson"
)

// Matcher is a composable predicate used in assertions to validate actual values
// against expected conditions.
type Matcher[T any] interface {
	// Check returns true if actual satisfies this matcher's condition.
	Check(actual T) bool
	// Expected returns a human-readable description of what was expected.
	Expected() string
}

// isMatcher validates exact value matching.
type isMatcher[T comparable] struct {
	value T
}

// Is creates a matcher that validates exact equality.
func Is[T comparable](value T) isMatcher[T] {
	return isMatcher[T]{value: value}
}

func (m isMatcher[T]) Check(actual T) bool {
	return actual == m.value
}

func (m isMatcher[T]) Expected() string {
	return fmt.Sprintf("%v", m.value)
}

// greaterThanMatcher validates that a value is greater than a reference value.
type greaterThanMatcher[T cmp.Ordered] struct {
	value T
}

// GreaterThan creates a matcher that asserts actual > value.
func GreaterThan[T cmp.Ordered](value T) greaterThanMatcher[T] {
	return greaterThanMatcher[T]{value: value}
}

func (m greaterThanMatcher[T]) Check(actual T) bool {
	return actual > m.value
}

func (m greaterThanMatcher[T]) Expected() string {
	return fmt.Sprintf("> %v", m.value)
}

// lessThanMatcher validates that a value is less than a reference value.
type lessThanMatcher[T cmp.Ordered] struct {
	value T
}

// LessThan creates a matcher that asserts actual < value.
func LessThan[T cmp.Ordered](value T) lessThanMatcher[T] {
	return lessThanMatcher[T]{value: value}
}

func (m lessThanMatcher[T]) Check(actual T) bool {
	return actual < m.value
}

func (m lessThanMatcher[T]) Expected() string {
	return fmt.Sprintf("< %v", m.value)
}

// isNullMatcher validates that a value is nil.
type isNullMatcher[T any] struct{}

// IsNull creates a matcher that checks if a value is nil.
func IsNull[T any]() isNullMatcher[T] {
	return isNullMatcher[T]{}
}

func (m isNullMatcher[T]) Check(actual T) bool {
	v := reflect.ValueOf(actual)
	if !v.IsValid() {
		return true
	}

	switch v.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Ptr, reflect.Slice:
		return v.IsNil()
	default:
		return false
	}
}

func (m isNullMatcher[T]) Expected() string {
	return "null"
}

// containsMatcher validates that a string contains a substring.
type containsMatcher struct {
	substring string
}

// Contains creates a matcher that checks if actual contains the substring.
func Contains(substring string) containsMatcher {
	return containsMatcher{substring: substring}
}

func (m containsMatcher) Check(actual string) bool {
	return strings.Contains(actual, m.substring)
}

func (m containsMatcher) Expected() string {
	return fmt.Sprintf("containing %q", m.substring)
}

// patternMatcher validates that a string matches a regex pattern.
type patternMatcher struct {
	pattern *regexp.Regexp
	raw     string
}

// Matches creates a matcher that checks if actual matches the regex pattern.
func Matches(pattern string) patternMatcher {
	compiled, err := regexp.Compile(pattern)
	if err != nil {
		panic(fmt.Sprintf("invalid regex pattern %q: %v", pattern, err))
	}

	return patternMatcher{pattern: compiled, raw: pattern}
}

func (m patternMatcher) Check(actual string) bool {
	return m.pattern.MatchString(actual)
}

func (m patternMatcher) Expected() string {
	display := strings.NewReplacer("\n", `\n`, "\r", `\r`, "\t", `\t`).Replace(m.raw)
	return fmt.Sprintf("matching pattern /%s/", display)
}

// hasLenMatcher validates that a value has a specific length.
type hasLenMatcher[T any] struct {
	length int
}

// HasLen creates a matcher that validates the length of arrays, slices, maps, channels, or strings.
func HasLen[T any](length int) hasLenMatcher[T] {
	return hasLenMatcher[T]{length: length}
}

func (m hasLenMatcher[T]) Check(actual T) bool {
	v := reflect.ValueOf(actual)

	switch v.Kind() {
	case reflect.Array, reflect.Chan, reflect.Map, reflect.Slice, reflect.String:
		return v.Len() == m.length
	default:
		return false
	}
}

func (m hasLenMatcher[T]) Expected() string {
	return fmt.Sprintf("length %d", m.length)
}

// oneOfMatcher validates value is one of several valid values.
type oneOfMatcher[T comparable] struct {
	values []T
}

// OneOf creates a matcher that accepts any of the provided values.
func OneOf[T comparable](values ...T) oneOfMatcher[T] {
	return oneOfMatcher[T]{values: values}
}

func (m oneOfMatcher[T]) Check(actual T) bool {
	for _, v := range m.values {
		if actual == v {
			return true
		}
	}

	return false
}

func (m oneOfMatcher[T]) Expected() string {
	if len(m.values) == 0 {
		return "one of []"
	}

	if len(m.values) <= 4 {
		return fmt.Sprintf("one of %v", m.values)
	}

	// Truncate for readability if too many options
	return fmt.Sprintf("one of [%v, %v, %v, ... and %d more]", m.values[0], m.values[1], m.values[2], len(m.values)-3)
}

// notMatcher negates another matcher.
type notMatcher[T any] struct {
	matcher Matcher[T]
}

// Not creates a matcher that negates another matcher.
func Not[T any](matcher Matcher[T]) notMatcher[T] {
	return notMatcher[T]{matcher: matcher}
}

func (m notMatcher[T]) Check(actual T) bool {
	return !m.matcher.Check(actual)
}

func (m notMatcher[T]) Expected() string {
	return fmt.Sprintf("not %s", m.matcher.Expected())
}

// JSONFieldMatcher pairs a gjson path with a matcher for that field.
type JSONFieldMatcher struct {
	path    string
	matcher Matcher[string]
}

// JSON creates a matcher that extracts a JSON field at the given path and validates it.
func JSON(path string, matcher Matcher[string]) JSONFieldMatcher {
	return JSONFieldMatcher{path: path, matcher: matcher}
}

func (m JSONFieldMatcher) Check(actual string) bool {
	result := gjson.Get(actual, m.path)

	switch m.matcher.(type) {
	case isNullMatcher[string]:
		return result.Type == gjson.Null
	case hasLenMatcher[string]:
		// For length checks, we need the actual Go value
		matcher := m.matcher.(hasLenMatcher[string])
		value := result.Value()
		return reflect.ValueOf(value).Len() == matcher.length
	default:
		// Most matchers work on the string representation
		if result.Type == gjson.Null {
			return false
		}

		return m.matcher.Check(result.String())
	}
}

func (m JSONFieldMatcher) Expected() string {
	return fmt.Sprintf("field %q: %s", m.path, m.matcher.Expected())
}

// checkAll returns true if all matchers pass for the given value.
// If onFail is provided, it's called with the first failing matcher.
func checkAll[T any](value T, matchers []Matcher[T], onFail func(Matcher[T], T)) bool {
	for _, matcher := range matchers {
		if !matcher.Check(value) {
			if onFail != nil {
				onFail(matcher, value)
			}

			return false
		}
	}

	return true
}
