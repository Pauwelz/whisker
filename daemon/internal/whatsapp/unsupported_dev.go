//go:build dev

package whatsapp

import (
	"fmt"
	"os"
	"slices"
	"strings"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// logUnsupported writes the type of a Message shown as unsupported to stderr. The Plugin puts stderr in the journal.
func logUnsupported(id string, m *waE2E.Message) { fmt.Fprintln(os.Stderr, unsupportedLine(id, m)) }

// unsupportedLine is the Message id and the sorted names of its top-level fields. It holds no text, JID or key.
func unsupportedLine(id string, m *waE2E.Message) string {
	var fields []string
	m.ProtoReflect().Range(func(fd protoreflect.FieldDescriptor, v protoreflect.Value) bool {
		name := string(fd.Name())
		if isSingleMessage(fd) {
			if paired := pairedMediaType(v.Message()); paired != "" {
				name += "(pairedMediaType=" + paired + ")"
			}
		}
		fields = append(fields, name)
		return true
	})
	slices.Sort(fields)
	return fmt.Sprintf("unsupported message %s: %s", id, strings.Join(fields, " "))
}

// pairedMediaType is the name of the first pairedMediaType set inside m, at any depth. Empty when none is set.
func pairedMediaType(m protoreflect.Message) (found string) {
	m.Range(func(fd protoreflect.FieldDescriptor, v protoreflect.Value) bool {
		switch {
		case fd.Name() == "pairedMediaType" && fd.Kind() == protoreflect.EnumKind:
			if ev := fd.Enum().Values().ByNumber(v.Enum()); ev != nil {
				found = string(ev.Name())
			}
		case isSingleMessage(fd):
			found = pairedMediaType(v.Message())
		}
		return found == ""
	})
	return found
}

func isSingleMessage(fd protoreflect.FieldDescriptor) bool {
	return fd.Kind() == protoreflect.MessageKind && !fd.IsList() && !fd.IsMap()
}
