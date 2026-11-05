// Package domain holds the rules of free-form colored tags, which never affect totals (D5).
package domain

import (
	"errors"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const nameMaxChars = 120

var hexColorPattern = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

var (
	ErrNotFound  = errors.New("tag not found")
	ErrNameTaken = errors.New("tag name is taken")
)

// Violation names one rejected value.
type Violation struct {
	Field   string
	Message string
}

// ValidationError lists every rejected value of a request.
type ValidationError struct {
	Violations []Violation
}

func (e *ValidationError) Error() string {
	return "tag values are not valid"
}

type Tag struct {
	ID        string
	Name      string
	Color     string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// NewTag is a valid tag that has not been stored yet.
type NewTag struct {
	Name  string
	Color string
}

func CreateNewTag(name, color string) (NewTag, error) {
	var violations []Violation
	checkedName, violations := checkName(name, violations)
	checkedColor, violations := checkColor(color, violations)
	if len(violations) > 0 {
		return NewTag{}, &ValidationError{Violations: violations}
	}
	return NewTag{Name: checkedName, Color: checkedColor}, nil
}

// Changes are the validated fields of an edit; nil leaves a field as it is.
type Changes struct {
	Name  *string
	Color *string
}

func NewChanges(name, color *string) (Changes, error) {
	var (
		changes    Changes
		violations []Violation
	)
	if name != nil {
		var checked string
		checked, violations = checkName(*name, violations)
		changes.Name = &checked
	}
	if color != nil {
		var checked string
		checked, violations = checkColor(*color, violations)
		changes.Color = &checked
	}
	if len(violations) > 0 {
		return Changes{}, &ValidationError{Violations: violations}
	}
	return changes, nil
}

func checkName(raw string, violations []Violation) (string, []Violation) {
	name := strings.TrimSpace(raw)
	length := utf8.RuneCountInString(name)
	if length < 1 || length > nameMaxChars {
		return "", append(violations, Violation{Field: "name", Message: "must have between 1 and 120 characters"})
	}
	// PostgreSQL refuses NUL in TEXT, and no control character belongs in a name.
	if strings.ContainsFunc(name, unicode.IsControl) {
		return "", append(violations, Violation{Field: "name", Message: "must not contain control characters"})
	}
	return name, violations
}

func checkColor(raw string, violations []Violation) (string, []Violation) {
	if !hexColorPattern.MatchString(raw) {
		return "", append(violations, Violation{Field: "color", Message: "must be a hex color like #3b82f6"})
	}
	return raw, violations
}
