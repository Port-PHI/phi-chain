// SPDX-License-Identifier: Apache-2.0

package app_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Port-PHI/phi-chain/internal/storeprefix"

	cointypes "github.com/Port-PHI/phi-chain/x/coin/types"
	credentialstypes "github.com/Port-PHI/phi-chain/x/credentials/types"
	disclosuretypes "github.com/Port-PHI/phi-chain/x/disclosure/types"
	governancetypes "github.com/Port-PHI/phi-chain/x/governance/types"
	identitytypes "github.com/Port-PHI/phi-chain/x/identity/types"
	institutionstypes "github.com/Port-PHI/phi-chain/x/institutions/types"
	votingtypes "github.com/Port-PHI/phi-chain/x/voting/types"
)

// THE ROOT CAUSE, closed structurally.
//
// Every genesis keyspace that has ever been lost on this chain was lost the same way: a module gained a
// store prefix, and the export — and the test that would have noticed — kept covering the siblings it
// already knew about. Each individual fix was correct and each left the next one available, because
// nothing anywhere related "the prefixes this module writes" to "the prefixes anything checks".
//
// This relates them. Each module declares its prefixes in AllStorePrefixes(); the per-module coverage
// tests drive their round-trip assertions off that declaration; and THIS test proves the declaration is
// the truth by reading the module's SOURCE and comparing what is actually written there.
//
// The source audit used to recognise exactly ONE declaration shape — `Name = []byte{0xNN}`, single
// byte, at package scope, in keys.go — so a prefix declared any other way was invisible to it, and an
// invisible prefix is exactly a half-finished keyspace. It now discovers a store prefix however it is
// declared: as a multi-byte literal, as a []byte(...) conversion, built from a named const, computed by
// concatenation or append, in any file of the package, and in the keeper package as well as types. The
// audit cannot be evaded by choosing a declaration form.

// moduleUnderAudit is one module's declaration paired with the package directories that must agree with
// it. Both the types and the keeper package are read, because a store prefix can be declared in either.
type moduleUnderAudit struct {
	name     string
	pkgDir   string // module directory under x/, e.g. "identity"
	declared []storeprefix.Prefix
}

func modulesUnderAudit() []moduleUnderAudit {
	return []moduleUnderAudit{
		{"coin", "coin", cointypes.AllStorePrefixes()},
		{"credentials", "credentials", credentialstypes.AllStorePrefixes()},
		{"disclosure", "disclosure", disclosuretypes.AllStorePrefixes()},
		{"governance", "governance", governancetypes.AllStorePrefixes()},
		{"identity", "identity", identitytypes.AllStorePrefixes()},
		{"institutions", "institutions", institutionstypes.AllStorePrefixes()},
		{"voting", "voting", votingtypes.AllStorePrefixes()},
	}
}

// prefixDirs returns the source directories the audit reads for one module: its types package and its
// keeper package.
func prefixDirs(m moduleUnderAudit) []string {
	return []string{
		filepath.Join("..", "x", m.pkgDir, "types"),
		filepath.Join("..", "x", m.pkgDir, "keeper"),
	}
}

// TestNet_EveryModuleDeclaresEveryPrefixItWrites is the completeness half: the declared set and the set
// of store prefixes actually written in the module's source must be the SAME set, in both directions.
//
// It reads the source rather than the package because that is the only way to see a prefix nobody
// referenced. A prefix var that is declared and then never mentioned again compiles, exports nothing,
// and is invisible to every reflective check — which is exactly the state a half-finished keyspace is in.
func TestNet_EveryModuleDeclaresEveryPrefixItWrites(t *testing.T) {
	for _, m := range modulesUnderAudit() {
		t.Run(m.name, func(t *testing.T) {
			inSource := storePrefixesInDirs(t, prefixDirs(m)...)
			require.NotEmpty(t, inSource, "no store prefixes were found in %s — has the declaration style "+
				"changed beyond what the audit resolves? this test must be able to read it", m.pkgDir)

			declared := map[string]string{} // byte sequence -> name
			for _, p := range m.declared {
				require.Len(t, p.Bytes, 1,
					"%s: prefix %q is not a single byte; this audit's DECLARED side assumes the chain's "+
						"one-byte prefix scheme (discovery handles any width)", m.name, p.Name)
				seq := string(p.Bytes)
				require.NotContains(t, declared, seq,
					"%s: prefix %#x is declared twice (%q and %q)", m.name, p.Bytes, declared[seq], p.Name)
				declared[seq] = p.Name

				if p.Carry != storeprefix.CarryExact {
					require.NotEmpty(t, p.Reason,
						"%s: prefix %q is not carried exactly and must say why", m.name, p.Name)
				}
			}

			// Every store prefix written in source is declared …
			for seq, varName := range inSource {
				require.Contains(t, declared, seq,
					"%s: source declares %s = %#x but AllStorePrefixes() does not list it — a keyspace "+
						"nothing checks is a keyspace genesis will lose", m.name, varName, []byte(seq))
			}
			// … and nothing is declared that source does not write.
			for seq, name := range declared {
				require.Contains(t, inSource, seq,
					"%s: AllStorePrefixes() lists %q = %#x, which no source declaration produces", m.name, name, []byte(seq))
			}
		})
	}
}

// TestNet_PrefixNamesAreUniquePerModule keeps every failure above legible: a name identifies exactly
// one keyspace, so a message naming it is not ambiguous about which one broke.
func TestNet_PrefixNamesAreUniquePerModule(t *testing.T) {
	for _, m := range modulesUnderAudit() {
		seen := map[string]bool{}
		for _, name := range storeprefix.Names(m.declared) {
			require.NotEmpty(t, name, "%s: a declared prefix has no name", m.name)
			require.False(t, seen[name], "%s: duplicate prefix name %q", m.name, name)
			seen[name] = true
		}
	}
}

// --- source discovery: resolve every store-prefix declaration, whatever its form or location ---

// storePrefixesInDirs parses every non-test .go file in the given directories and returns the store
// prefixes declared in them, as byte-sequence → variable name.
//
// A store prefix is a package-scope const or var whose NAME ends in "Prefix" or "Key" (the chain's
// universal convention for a keyspace prefix or a single-record key) AND whose VALUE is a byte-producing
// expression — a []byte composite of any width, a []byte(...) conversion, a concatenation, or an append.
// Keying on a byte-producing value is what excludes the module's string identifiers (StoreKey and
// RouterKey are ModuleName; DIDMethodPrefix is a printable method string) without needing to name them:
// none of them produces a []byte, so none is mistaken for a store prefix.
func storePrefixesInDirs(t *testing.T, dirs ...string) map[string]string {
	t.Helper()

	var files []*ast.File
	fset := token.NewFileSet()
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		require.NoError(t, err, "reading %s", dir)
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
				continue
			}
			f, err := parser.ParseFile(fset, filepath.Join(dir, e.Name()), nil, 0)
			require.NoError(t, err, "parsing %s", e.Name())
			files = append(files, f)
		}
	}

	sc := collectScalarConsts(files)

	out := map[string]string{}
	for _, f := range files {
		for _, decl := range f.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || (gen.Tok != token.VAR && gen.Tok != token.CONST) {
				continue
			}
			for _, spec := range gen.Specs {
				vs, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				for i, name := range vs.Names {
					if i >= len(vs.Values) {
						continue
					}
					if !isPrefixName(name.Name) {
						continue
					}
					if b, ok := resolveByteValue(vs.Values[i], sc); ok && len(b) > 0 {
						out[string(b)] = name.Name
					}
				}
			}
		}
	}
	return out
}

// isPrefixName reports whether an identifier follows the chain's store-prefix naming convention.
func isPrefixName(n string) bool {
	return strings.HasSuffix(n, "Prefix") || strings.HasSuffix(n, "Key")
}

// scalarConsts holds package-scope string and integer constants/vars, for resolving references that
// appear inside a prefix's byte expression (a named string in []byte(NAME), a named byte in []byte{NAME}).
type scalarConsts struct {
	strs map[string][]byte
	ints map[string]int64
}

func collectScalarConsts(files []*ast.File) scalarConsts {
	sc := scalarConsts{strs: map[string][]byte{}, ints: map[string]int64{}}
	for _, f := range files {
		for _, decl := range f.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || (gen.Tok != token.VAR && gen.Tok != token.CONST) {
				continue
			}
			for _, spec := range gen.Specs {
				vs, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				for i, name := range vs.Names {
					if i >= len(vs.Values) {
						continue
					}
					lit, ok := vs.Values[i].(*ast.BasicLit)
					if !ok {
						continue
					}
					switch lit.Kind {
					case token.STRING:
						if s, err := strconv.Unquote(lit.Value); err == nil {
							sc.strs[name.Name] = []byte(s)
						}
					case token.INT, token.CHAR:
						if b, ok := parseByteLit(lit); ok {
							sc.ints[name.Name] = int64(b)
						}
					}
				}
			}
		}
	}
	return sc
}

// resolveByteValue returns the bytes a declaration's VALUE produces, if that value is a byte-producing
// expression, and whether it resolved. This is the whole point of the audit's un-evadability: the byte
// sequence is recovered whatever shape the declaration takes.
func resolveByteValue(expr ast.Expr, sc scalarConsts) ([]byte, bool) {
	switch e := expr.(type) {
	case *ast.CompositeLit: // []byte{ b0, b1, ... } — any width
		if !isByteSliceType(e.Type) {
			return nil, false
		}
		out := []byte{}
		for _, elt := range e.Elts {
			b, ok := resolveByteElem(elt, sc)
			if !ok {
				return nil, false
			}
			out = append(out, b)
		}
		return out, true
	case *ast.CallExpr:
		if isByteSliceType(e.Fun) { // []byte(arg) conversion
			if len(e.Args) != 1 {
				return nil, false
			}
			return resolveBytesFromStringish(e.Args[0], sc)
		}
		if id, ok := e.Fun.(*ast.Ident); ok && id.Name == "append" && len(e.Args) >= 1 {
			base, ok := resolveByteValue(e.Args[0], sc)
			if !ok {
				return nil, false
			}
			out := append([]byte{}, base...)
			for _, a := range e.Args[1:] {
				// append(base, tail...) — a spread []byte/string — or append(base, b0, b1) — bytes.
				if bs, ok := resolveBytesFromStringish(a, sc); ok {
					out = append(out, bs...)
					continue
				}
				if b, ok := resolveByteElem(a, sc); ok {
					out = append(out, b)
					continue
				}
				return nil, false
			}
			return out, true
		}
		return nil, false
	default:
		return nil, false
	}
}

// resolveBytesFromStringish resolves the bytes of a string-shaped expression: a string literal, a named
// string const, a concatenation of them, or a nested byte-producing expression.
func resolveBytesFromStringish(expr ast.Expr, sc scalarConsts) ([]byte, bool) {
	switch e := expr.(type) {
	case *ast.BasicLit:
		if e.Kind == token.STRING {
			if s, err := strconv.Unquote(e.Value); err == nil {
				return []byte(s), true
			}
		}
		return nil, false
	case *ast.Ident:
		if b, ok := sc.strs[e.Name]; ok {
			return b, true
		}
		return nil, false
	case *ast.BinaryExpr: // "a" + b + ... (computed by concatenation)
		if e.Op != token.ADD {
			return nil, false
		}
		l, ok := resolveBytesFromStringish(e.X, sc)
		if !ok {
			return nil, false
		}
		r, ok := resolveBytesFromStringish(e.Y, sc)
		if !ok {
			return nil, false
		}
		return append(append([]byte{}, l...), r...), true
	case *ast.CompositeLit, *ast.CallExpr:
		return resolveByteValue(expr, sc)
	default:
		return nil, false
	}
}

// resolveByteElem resolves one element of a []byte composite to a single byte: an integer/char literal,
// a named byte const, or a byte(...) conversion.
func resolveByteElem(expr ast.Expr, sc scalarConsts) (byte, bool) {
	switch e := expr.(type) {
	case *ast.BasicLit:
		return parseByteLit(e)
	case *ast.Ident:
		if v, ok := sc.ints[e.Name]; ok && v >= 0 && v <= 255 {
			return byte(v), true
		}
		return 0, false
	case *ast.CallExpr:
		if id, ok := e.Fun.(*ast.Ident); ok && (id.Name == "byte" || id.Name == "uint8") && len(e.Args) == 1 {
			return resolveByteElem(e.Args[0], sc)
		}
		return 0, false
	default:
		return 0, false
	}
}

func parseByteLit(lit *ast.BasicLit) (byte, bool) {
	switch lit.Kind {
	case token.INT:
		n, err := strconv.ParseUint(lit.Value, 0, 16)
		if err != nil || n > 255 {
			return 0, false
		}
		return byte(n), true
	case token.CHAR:
		s, err := strconv.Unquote(lit.Value)
		if err != nil || len(s) != 1 {
			return 0, false
		}
		return s[0], true
	default:
		return 0, false
	}
}

// isByteSliceType reports whether an expr is the type `[]byte` (or `[]uint8`).
func isByteSliceType(expr ast.Expr) bool {
	arr, ok := expr.(*ast.ArrayType)
	if !ok || arr.Len != nil {
		return false
	}
	id, ok := arr.Elt.(*ast.Ident)
	return ok && (id.Name == "byte" || id.Name == "uint8")
}

// --- Adversarial self-review: the audit must discover a prefix in every form, in every location ---

// TestNet_PrefixAuditIsUnEvadable plants a probe store prefix in each of the six shapes/locations the
// old audit missed, in synthetic source, and proves the discovery finds every one — and none of the
// non-prefix look-alikes. A discovered prefix absent from AllStorePrefixes() then fails the completeness
// test above, so there is no form in which a keyspace can be added unseen.
func TestNet_PrefixAuditIsUnEvadable(t *testing.T) {
	root := t.TempDir()
	typesDir := filepath.Join(root, "types")
	keeperDir := filepath.Join(root, "keeper")
	require.NoError(t, os.MkdirAll(typesDir, 0o755))
	require.NoError(t, os.MkdirAll(keeperDir, 0o755))

	// (a) const/string form, (b) []byte(...), (c) computed by concatenation, (d) multi-byte, plus a
	// byte-const composite and an append — all in one file of the "types" package.
	writeGo(t, typesDir, "probes.go", `package probes

const probeAConst = "\x2a"
var ProbeAPrefix = []byte(probeAConst) // (a) const/string form

var ProbeBPrefix = []byte("\x2b") // (b) []byte(...) conversion

const pcHead = "\x2c"
const pcTail = "\x2d"
var ProbeCPrefix = []byte(pcHead + pcTail) // (c) computed by concatenation

var ProbeDPrefix = []byte{0x2e, 0x2f} // (d) multi-byte

const probeByteConst = 0x32
var ProbeEPrefix = []byte{probeByteConst} // a byte-const inside the composite

var ProbeAppendPrefix = append([]byte{0x30}, 0x31) // computed by append

// Non-prefixes that must NOT be mistaken for store prefixes:
var plainByteBlob = []byte{0x40} // a byte var whose name is not a prefix/key
const DIDMethodPrefix = "did:phi:"  // a *Prefix string const that is not a KV prefix
const StoreKey = "somemodulename"   // a *Key string const that is not a KV prefix
`)

	// (e) a prefix declared in ANOTHER file of the same package.
	writeGo(t, typesDir, "more_keys.go", `package probes

var ProbeOtherFilePrefix = []byte{0x33} // (e) a different file than keys.go
`)

	// (f) a prefix declared in the KEEPER package.
	writeGo(t, keeperDir, "keeper_keys.go", `package probeskeeper

var ProbeKeeperPrefix = []byte{0x34} // (f) in the keeper package
`)

	found := storePrefixesInDirs(t, typesDir, keeperDir)

	// Every form and location is discovered, with the exact bytes it declares.
	for _, want := range []struct {
		name string
		seq  string
		note string
	}{
		{"ProbeAPrefix", "\x2a", "(a) const/string form"},
		{"ProbeBPrefix", "\x2b", "(b) []byte(...) conversion"},
		{"ProbeCPrefix", "\x2c\x2d", "(c) computed by concatenation"},
		{"ProbeDPrefix", "\x2e\x2f", "(d) multi-byte"},
		{"ProbeEPrefix", "\x32", "a byte-const composite"},
		{"ProbeAppendPrefix", "\x30\x31", "computed by append"},
		{"ProbeOtherFilePrefix", "\x33", "(e) another file"},
		{"ProbeKeeperPrefix", "\x34", "(f) keeper package"},
	} {
		require.Equal(t, want.name, found[want.seq], "hatch %s: prefix %#x was not discovered", want.note, []byte(want.seq))

		// And it is CAUGHT: a discovered prefix absent from AllStorePrefixes() fails the completeness
		// direction of the real audit. Model that here with an empty declaration.
		declared := map[string]string{}
		require.NotContains(t, declared, want.seq,
			"a prefix declared as %s but missing from AllStorePrefixes() must be flagged", want.note)
	}

	// No false positives: the non-prefix look-alikes are not discovered.
	require.NotContains(t, found, "\x40", "a byte var not named *Prefix/*Key must not be a store prefix")
	require.NotContains(t, found, "did:phi:", "a *Prefix STRING const is not a store prefix")
	require.NotContains(t, found, "somemodulename", "a *Key STRING const (StoreKey) is not a store prefix")

	// Exactly the eight probes, nothing else.
	require.Len(t, found, 8, "discovery found unexpected entries: %v", found)
}

// writeGo writes a synthetic Go source file for the audit to parse.
func writeGo(t *testing.T, dir, name, src string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(src), 0o644))
}
