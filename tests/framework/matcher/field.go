package matcher

import (
	"errors"
	"fmt"
	"reflect"
	"strings"

	"github.com/onsi/gomega/types"
)

const (
	passedFieldsNotEmpty = "Passed in fields may not be empty"
	fieldNotFoundFmt     = "Field '%s' was not found"
	accessorErrorFmt     = "%v accessor error: %v is of the type %v, expected reflect.Struct"
)

func Field(matcher types.GomegaMatcher, fields ...string) types.GomegaMatcher {
	return fieldMatcher{
		fields:  fields,
		matcher: matcher,
	}
}

type fieldMatcher struct {
	fields  []string
	matcher types.GomegaMatcher
}

func (c fieldMatcher) Match(actual interface{}) (bool, error) {
	if len(c.fields) < 1 {
		return false, errors.New(passedFieldsNotEmpty)
	}

	val, found, err := nestedField(actual, c.fields)
	if err != nil {
		return false, err
	}
	if !found {
		return false, nil
	}

	return c.matcher.Match(val)
}

func nestedField(val interface{}, fields []string) (interface{}, bool, error) {
	if val == nil {
		return nil, false, nil
	}

	reflected := reflect.ValueOf(val)
	for i, field := range fields {
		if reflected.Kind() == reflect.Ptr {
			if elem := reflected.Elem(); elem.IsValid() {
				reflected = elem
			} else {
				return nil, false, nil
			}
		}
		if reflected.Kind() == reflect.Struct {
			if fieldByName := reflected.FieldByName(field); fieldByName.IsValid() {
				reflected = fieldByName
			} else {
				return nil, false, fmt.Errorf(fieldNotFoundFmt, strings.Join(fields[:i+1], "."))
			}
		} else {
			return nil, false, fmt.Errorf(accessorErrorFmt, strings.Join(fields[:i+1], "."), reflected, reflected.Kind())
		}
	}

	return reflected.Interface(), true, nil
}

func (c fieldMatcher) FailureMessage(actual interface{}) string {
	return c.failMessage(actual, false)
}

func (c fieldMatcher) NegatedFailureMessage(actual interface{}) string {
	return c.failMessage(actual, true)
}

func (c fieldMatcher) failMessage(actual interface{}, negate bool) string {
	if len(c.fields) < 1 {
		return passedFieldsNotEmpty
	}

	val, _, err := nestedField(actual, c.fields)
	if err != nil {
		return err.Error()
	}

	if negate {
		return c.matcher.NegatedFailureMessage(val)
	}
	return c.matcher.FailureMessage(val)
}
