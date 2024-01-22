// Code generated by 'yaegi extract encoding/ascii85'. DO NOT EDIT.

//go:build go1.19 && !go1.20
// +build go1.19,!go1.20

package stdlib

import (
	"encoding/ascii85"
	"reflect"
)

func init() {
	Symbols["encoding/ascii85/ascii85"] = map[string]reflect.Value{
		// function, constant and variable definitions
		"Decode":        reflect.ValueOf(ascii85.Decode),
		"Encode":        reflect.ValueOf(ascii85.Encode),
		"MaxEncodedLen": reflect.ValueOf(ascii85.MaxEncodedLen),
		"NewDecoder":    reflect.ValueOf(ascii85.NewDecoder),
		"NewEncoder":    reflect.ValueOf(ascii85.NewEncoder),

		// type definitions
		"CorruptInputError": reflect.ValueOf((*ascii85.CorruptInputError)(nil)),
	}
}