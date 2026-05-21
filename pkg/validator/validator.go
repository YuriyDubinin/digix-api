package validator

import (
	"errors"
	"fmt"
	"reflect"
	"strings"

	goplay "github.com/go-playground/validator/v10"
)

type Validator struct {
	v *goplay.Validate
}

func New() *Validator {
	v := goplay.New(goplay.WithRequiredStructEnabled())

	// Сообщения и FieldError.Field используют JSON-имя поля, а не Go-имя.
	v.RegisterTagNameFunc(func(fld reflect.StructField) string {
		name := strings.SplitN(fld.Tag.Get("json"), ",", 2)[0]
		if name == "-" {
			return ""
		}
		return name
	})

	return &Validator{v: v}
}

func (vd *Validator) Struct(s any) error {
	return vd.v.Struct(s)
}

type FieldError struct {
	Field   string
	Message string
}

func TranslateErrors(err error) []FieldError {
	var verrs goplay.ValidationErrors
	if !errors.As(err, &verrs) {
		return nil
	}
	out := make([]FieldError, 0, len(verrs))
	for _, fe := range verrs {
		out = append(out, FieldError{
			Field:   fe.Field(),
			Message: messageFor(fe),
		})
	}
	return out
}

func messageFor(fe goplay.FieldError) string {
	switch fe.Tag() {
	case "required":
		return "is required"
	case "email":
		return "must be a valid email"
	case "min":
		return fmt.Sprintf("must be at least %s characters", fe.Param())
	case "max":
		return fmt.Sprintf("must be at most %s characters", fe.Param())
	default:
		return fmt.Sprintf("failed %q validation", fe.Tag())
	}
}
