package contract_test

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Pauwelz/whisker/daemon/internal/contract"
)

const fixtureGlob = "../../../contract/fixtures/*.jsonl"

type fixtureLine struct {
	Dir   string          `json:"dir"`
	Frame json.RawMessage `json:"frame"`
}

// run feeds the "in" lines of a fixture to the Daemon and returns its exit code, stdout and stderr.
func run(t *testing.T, input string) (int, string, string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code := contract.Run(strings.NewReader(input), &out, &errOut)
	return code, out.String(), errOut.String()
}

func TestFixtures(t *testing.T) {
	paths, err := filepath.Glob(fixtureGlob)
	if err != nil || len(paths) == 0 {
		t.Fatalf("no fixtures found: %v", err)
	}
	for _, path := range paths {
		t.Run(filepath.Base(path), func(t *testing.T) {
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			var input strings.Builder
			var want []any
			sc := bufio.NewScanner(bytes.NewReader(raw))
			for sc.Scan() {
				var line fixtureLine
				if err := json.Unmarshal(sc.Bytes(), &line); err != nil {
					t.Fatalf("bad fixture line %q: %v", sc.Text(), err)
				}
				switch line.Dir {
				case "in":
					var text string
					if json.Unmarshal(line.Frame, &text) != nil {
						text = string(line.Frame) // an object frame is sent as it is
					}
					input.WriteString(text + "\n")
				case "out":
					var frame any
					if err := json.Unmarshal(line.Frame, &frame); err != nil {
						t.Fatal(err)
					}
					want = append(want, frame)
				}
				// "view" lines belong to the Plugin tests.
			}

			code, stdout, _ := run(t, input.String())
			if code != contract.ExitOK {
				t.Errorf("exit code = %d, want %d", code, contract.ExitOK)
			}
			var got []any
			for _, l := range strings.Split(strings.TrimSpace(stdout), "\n") {
				var frame any
				if err := json.Unmarshal([]byte(l), &frame); err != nil {
					t.Fatalf("stdout line is not JSON: %q", l)
				}
				got = append(got, frame)
			}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("frames = %v, want %v", got, want)
			}
		})
	}
}

func TestOversizedLineExitsWithDistinctCode(t *testing.T) {
	line := strings.Repeat("a", contract.MaxLineBytes+1) + "\n"
	code, stdout, stderr := run(t, line)
	if code != contract.ExitOversizedLine {
		t.Errorf("exit code = %d, want %d", code, contract.ExitOversizedLine)
	}
	if strings.Count(stdout, "\n") != 1 { // only the ready frame
		t.Errorf("stdout = %q, want only the ready frame", stdout)
	}
	if !strings.Contains(stderr, "line too long") {
		t.Errorf("stderr = %q, want an error", stderr)
	}
	if strings.Contains(stderr, "aaaa") {
		t.Error("stderr must not echo the line")
	}
}

func TestContractDocListsExitCodes(t *testing.T) {
	doc, err := os.ReadFile("../../../docs/contract.md")
	if err != nil {
		t.Fatal(err)
	}
	for _, code := range []int{contract.ExitOK, contract.ExitError, contract.ExitOversizedLine, contract.ExitProfileInUse} {
		if !strings.Contains(string(doc), fmt.Sprintf("| %d |", code)) {
			t.Errorf("docs/contract.md has no row for exit code %d", code)
		}
	}
}
