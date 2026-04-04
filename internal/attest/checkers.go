package attest

import (
	"cmp"
	"fmt"
	"reflect"
	"regexp"
	"strings"

	"github.com/tidwall/gjson"
)

// Checker is a composable predicate used in assertions to validate actual values
// against expected conditions.
type Checker[T any] interface {
	// Check returns true if actual satisfies this checker's condition.
	Check(actual T) bool
	// Expected returns a human-readable description of what was expected.
	Expected() string
}

// isChecker validates exact value matching.
type isChecker[T comparable] struct {
	value T
}

// Is creates a checker that validates exact equality.
func Is[T comparable](value T) isChecker[T] {
	return isChecker[T]{value: value}
}

func (m isChecker[T]) Check(actual T) bool {
	return actual == m.value
}

func (m isChecker[T]) Expected() string {
	return fmt.Sprintf("%v", m.value)
}

// greaterThanChecker validates that a value is greater than a reference value.
type greaterThanChecker[T cmp.Ordered] struct {
	value T
}

// GreaterThan creates a checker that asserts actual > value.
func GreaterThan[T cmp.Ordered](value T) greaterThanChecker[T] {
	return greaterThanChecker[T]{value: value}
}

func (m greaterThanChecker[T]) Check(actual T) bool {
	return actual > m.value
}

func (m greaterThanChecker[T]) Expected() string {
	return fmt.Sprintf("> %v", m.value)
}

// lessThanChecker validates that a value is less than a reference value.
type lessThanChecker[T cmp.Ordered] struct {
	value T
}

// LessThan creates a checker that asserts actual < value.
func LessThan[T cmp.Ordered](value T) lessThanChecker[T] {
	return lessThanChecker[T]{value: value}
}

func (m lessThanChecker[T]) Check(actual T) bool {
	return actual < m.value
}

func (m lessThanChecker[T]) Expected() string {
	return fmt.Sprintf("< %v", m.value)
}

// isNullChecker validates that a value is nil.
type isNullChecker[T any] struct{}

// IsNull creates a checker that checks if a value is nil.
func IsNull[T any]() isNullChecker[T] {
	return isNullChecker[T]{}
}

func (m isNullChecker[T]) Check(actual T) bool {
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

func (m isNullChecker[T]) Expected() string {
	return "null"
}

// containsChecker validates that a string contains a substring.
type containsChecker struct {
	substring string
}

// Contains creates a checker that checks if actual contains the substring.
func Contains(substring string) containsChecker {
	return containsChecker{substring: substring}
}

func (m containsChecker) Check(actual string) bool {
	return strings.Contains(actual, m.substring)
}

func (m containsChecker) Expected() string {
	return fmt.Sprintf("containing %q", m.substring)
}

// matchesChecker validates that a string matches a regex pattern.
type matchesChecker struct {
	pattern *regexp.Regexp
	raw     string
}

// Matches creates a checker that checks if actual matches the regex pattern.
func Matches(pattern string) matchesChecker {
	compiled, err := regexp.Compile(pattern)
	if err != nil {
		panic(fmt.Sprintf("invalid regex pattern %q: %v", pattern, err))
	}

	return matchesChecker{pattern: compiled, raw: pattern}
}

func (m matchesChecker) Check(actual string) bool {
	return m.pattern.MatchString(actual)
}

func (m matchesChecker) Expected() string {
	return fmt.Sprintf("matching pattern %q", m.raw)
}

// hasLenChecker validates that a value has a specific length.
type hasLenChecker[T any] struct {
	length int
}

// HasLen creates a checker that validates the length of arrays, slices, maps, channels, or strings.
func HasLen[T any](length int) hasLenChecker[T] {
	return hasLenChecker[T]{length: length}
}

func (m hasLenChecker[T]) Check(actual T) bool {
	v := reflect.ValueOf(actual)

	switch v.Kind() {
	case reflect.Array, reflect.Chan, reflect.Map, reflect.Slice, reflect.String:
		return v.Len() == m.length
	default:
		return false
	}
}

func (m hasLenChecker[T]) Expected() string {
	return fmt.Sprintf("length %d", m.length)
}

// oneOfChecker validates value is one of several valid values.
type oneOfChecker[T comparable] struct {
	values []T
}

// OneOf creates a checker that accepts any of the provided values.
func OneOf[T comparable](values ...T) oneOfChecker[T] {
	return oneOfChecker[T]{values: values}
}

func (m oneOfChecker[T]) Check(actual T) bool {
	for _, v := range m.values {
		if actual == v {
			return true
		}
	}

	return false
}

func (m oneOfChecker[T]) Expected() string {
	if len(m.values) == 0 {
		return "one of []"
	}

	if len(m.values) <= 4 {
		return fmt.Sprintf("one of %v", m.values)
	}

	// Truncate for readability if too many options
	return fmt.Sprintf("one of [%v, %v, %v, ... and %d more]", m.values[0], m.values[1], m.values[2], len(m.values)-3)
}

// notChecker negates another checker.
type notChecker[T any] struct {
	checker Checker[T]
}

// Not creates a checker that negates another checker.
func Not[T any](checker Checker[T]) notChecker[T] {
	return notChecker[T]{checker: checker}
}

func (m notChecker[T]) Check(actual T) bool {
	return !m.checker.Check(actual)
}

func (m notChecker[T]) Expected() string {
	return fmt.Sprintf("not %s", m.checker.Expected())
}

// JSONFieldChecker pairs a gjson path with a checker for that field.
type JSONFieldChecker struct {
	path    string
	checker Checker[string]
}

// JSON creates a checker that extracts a JSON field at the given path and validates it.
func JSON(path string, checker Checker[string]) JSONFieldChecker {
	return JSONFieldChecker{path: path, checker: checker}
}

func (m JSONFieldChecker) Check(actual string) bool {
	result := gjson.Get(actual, m.path)

	switch m.checker.(type) {
	case isNullChecker[string]:
		return result.Type == gjson.Null
	case hasLenChecker[string]:
		// For length checks, we need the actual Go value
		checker := m.checker.(hasLenChecker[string])
		value := result.Value()
		return reflect.ValueOf(value).Len() == checker.length
	default:
		// Most checkers work on the string representation
		if result.Type == gjson.Null {
			return false
		}

		return m.checker.Check(result.String())
	}
}

func (m JSONFieldChecker) Expected() string {
	return fmt.Sprintf("field %s: %s", m.path, m.checker.Expected())
}

// checkAll returns true if all checkers pass for the given value.
// If onFail is provided, it's called with the first failing checker.
func checkAll[T any](value T, checkers []Checker[T], onFail func(Checker[T], T)) bool {
	for _, checker := range checkers {
		if !checker.Check(value) {
			if onFail != nil {
				onFail(checker, value)
			}

			return false
		}
	}

	return true
}
