package core

import "strings"

type PhoneNumber struct {
	raw string
}

func NewPhoneNumber(raw string) PhoneNumber {
	return PhoneNumber{raw: strings.TrimSpace(raw)}
}

func (p PhoneNumber) String() string {
	return p.raw
}

func (p PhoneNumber) IsEmpty() bool {
	return p.raw == ""
}
