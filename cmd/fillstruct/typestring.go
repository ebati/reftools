// Copyright (c) 2017 David R. Jenni. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Plundered from go/types; customized.

// Copyright (c) 2009 The Go Authors. All rights reserved.
//
// Redistribution and use in source and binary forms, with or without
// modification, are permitted provided that the following conditions are
// met:
//
//    * Redistributions of source code must retain the above copyright
// notice, this list of conditions and the following disclaimer.
//    * Redistributions in binary form must reproduce the above
// copyright notice, this list of conditions and the following disclaimer
// in the documentation and/or other materials provided with the
// distribution.
//    * Neither the name of Google Inc. nor the names of its
// contributors may be used to endorse or promote products derived from
// this software without specific prior written permission.
//
// THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS
// "AS IS" AND ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT
// LIMITED TO, THE IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR
// A PARTICULAR PURPOSE ARE DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT
// OWNER OR CONTRIBUTORS BE LIABLE FOR ANY DIRECT, INDIRECT, INCIDENTAL,
// SPECIAL, EXEMPLARY, OR CONSEQUENTIAL DAMAGES (INCLUDING, BUT NOT
// LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR SERVICES; LOSS OF USE,
// DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER CAUSED AND ON ANY
// THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY, OR TORT
// (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE
// OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.

// This file implements printing of types.

package main

import (
	"bytes"
	"errors"
	"fmt"
	"go/types"
)

func typeString(pkg *types.Package, typ types.Type) (string, error) {
	var buf bytes.Buffer
	err := writeType(&buf, pkg, typ, make([]types.Type, 0, 8))
	return buf.String(), err
}

func writeType(buf *bytes.Buffer, pkg *types.Package, typ types.Type, visited []types.Type) error {
	// Theoretically, this is a quadratic lookup algorithm, but in
	// practice deeply nested composite types with unnamed component
	// types are uncommon. This code is likely more efficient than
	// using a map.
	for _, t := range visited {
		if t == typ {
			fmt.Fprintf(buf, "○%T", typ) // cycle to typ
			return nil
		}
	}
	visited = append(visited, typ)

	switch t := typ.(type) {
	case nil:
		buf.WriteString("nil")
		return nil

	case *types.Basic:
		if t.Kind() == types.Invalid {
			return errors.New("TODO")
		}
		if t.Kind() == types.UnsafePointer {
			buf.WriteString("unsafe.")
		}
		buf.WriteString(t.Name())
		return nil

	case *types.Array:
		fmt.Fprintf(buf, "[%d]", t.Len())
		return writeType(buf, pkg, t.Elem(), visited)

	case *types.Slice:
		buf.WriteString("[]")
		return writeType(buf, pkg, t.Elem(), visited)

	case *types.Struct:
		buf.WriteString("struct{")
		for i := 0; i < t.NumFields(); i++ {
			f := t.Field(i)
			if i > 0 {
				buf.WriteString("; ")
			}
			if !f.Anonymous() {
				buf.WriteString(f.Name())
				buf.WriteByte(' ')
			}
			if err := writeType(buf, pkg, f.Type(), visited); err != nil {
				return err
			}
			if tag := t.Tag(i); tag != "" {
				fmt.Fprintf(buf, " %q", tag)
			}
		}
		buf.WriteByte('}')
		return nil

	case *types.Pointer:
		buf.WriteByte('*')
		return writeType(buf, pkg, t.Elem(), visited)

	case *types.Tuple:
		return writeTuple(buf, pkg, t, false, visited)

	case *types.Signature:
		buf.WriteString("func")
		return writeSignature(buf, pkg, t, visited)

	case *types.Interface:
		// We write the source-level methods and embedded types rather
		// than the actual method set since resolved method signatures
		// may have non-printable cycles if parameters have anonymous
		// interface types that (directly or indirectly) embed the
		// current interface. For instance, consider the result type
		// of m:
		//
		//     type T interface{
		//         m() interface{ T }
		//     }
		//
		buf.WriteString("interface{")
		// print explicit interface methods and embedded types
		for i := 0; i < t.NumMethods(); i++ {
			m := t.Method(i)
			if i > 0 {
				buf.WriteString("; ")
			}
			buf.WriteString(m.Name())
			if err := writeSignature(buf, pkg, m.Type().(*types.Signature), visited); err != nil {
				return err
			}
		}
		for i := 0; i < t.NumEmbeddeds(); i++ {
			if i > 0 || t.NumMethods() > 0 {
				buf.WriteString("; ")
			}
			if err := writeType(buf, pkg, t.Embedded(i), visited); err != nil {
				return err
			}
		}
		buf.WriteByte('}')
		return nil

	case *types.Map:
		buf.WriteString("map[")
		if err := writeType(buf, pkg, t.Key(), visited); err != nil {
			return err
		}
		buf.WriteByte(']')
		return writeType(buf, pkg, t.Elem(), visited)

	case *types.Chan:
		var s string
		var parens bool
		switch t.Dir() {
		case types.SendRecv:
			s = "chan "
			// chan (<-chan T) requires parentheses
			if c, _ := t.Elem().(*types.Chan); c != nil && c.Dir() == types.RecvOnly {
				parens = true
			}
		case types.SendOnly:
			s = "chan<- "
		case types.RecvOnly:
			s = "<-chan "
		default:
			panic("unreachable")
		}
		buf.WriteString(s)
		if parens {
			buf.WriteByte('(')
		}
		if err := writeType(buf, pkg, t.Elem(), visited); err != nil {
			return err
		}
		if parens {
			buf.WriteByte(')')
		}
		return nil

	case *types.Named:
		if isImported(pkg, t) && t.Obj().Pkg() != nil {
			buf.WriteString(fmt.Sprintf("%s.%s", t.Obj().Pkg().Name(), t.Obj().Name()))
		} else {
			buf.WriteString(t.Obj().Name())
		}
		return nil

	default:
		// For externally defined implementations of Type.
		buf.WriteString(t.String())
		return nil
	}
}

func writeTuple(buf *bytes.Buffer, pkg *types.Package, tup *types.Tuple, variadic bool, visited []types.Type) error {
	buf.WriteByte('(')
	if tup != nil {
		for i := 0; i < tup.Len(); i++ {
			v := tup.At(i)
			if i > 0 {
				buf.WriteString(", ")
			}
			if v.Name() != "" {
				buf.WriteString(v.Name())
				buf.WriteByte(' ')
			}
			typ := v.Type()
			if variadic && i == tup.Len()-1 {
				if s, ok := typ.(*types.Slice); ok {
					buf.WriteString("...")
					typ = s.Elem()
				} else {
					// special case:
					// append(s, "foo"...) leads to signature func([]byte, string...)
					if t, ok := typ.Underlying().(*types.Basic); !ok || t.Kind() != types.String {
						panic("internal error: string type expected")
					}
					writeType(buf, pkg, typ, visited)
					buf.WriteString("...")
					continue
				}
			}
			writeType(buf, pkg, typ, visited)
		}
	}
	buf.WriteByte(')')
	return nil
}

func writeSignature(buf *bytes.Buffer, pkg *types.Package, sig *types.Signature, visited []types.Type) error {
	writeTuple(buf, pkg, sig.Params(), sig.Variadic(), visited)

	n := sig.Results().Len()
	if n == 0 {
		return nil // no result
	}

	buf.WriteByte(' ')
	if n == 1 && sig.Results().At(0).Name() == "" {
		// single unnamed result
		return writeType(buf, pkg, sig.Results().At(0).Type(), visited)
	}

	// multiple or named result(s)
	return writeTuple(buf, pkg, sig.Results(), false, visited)
}
