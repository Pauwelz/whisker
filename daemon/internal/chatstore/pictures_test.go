package chatstore_test

import (
	"slices"
	"testing"
)

func TestPictureCache(t *testing.T) {
	st, _ := newStore(t)
	if _, ok, err := st.Picture("1@s.whatsapp.net"); err != nil || ok {
		t.Fatalf("empty cache = ok %v, err %v", ok, err)
	}
	if err := st.PutPicture("1@s.whatsapp.net", "p1", []byte("JPEG")); err != nil {
		t.Fatal(err)
	}
	p, ok, err := st.Picture("1@s.whatsapp.net")
	if err != nil || !ok || p.ID != "p1" || !slices.Equal(p.Data, []byte("JPEG")) || p.FetchedAt != now.Unix() {
		t.Fatalf("picture = %+v, ok %v, err %v", p, ok, err)
	}
	if err := st.PutPicture("1@s.whatsapp.net", "", nil); err != nil { // replaces: no picture
		t.Fatal(err)
	}
	if p, ok, _ := st.Picture("1@s.whatsapp.net"); !ok || p.ID != "" || len(p.Data) != 0 {
		t.Fatalf("no-picture row = %+v, ok %v", p, ok)
	}
	if err := st.DeletePicture("1@s.whatsapp.net"); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := st.Picture("1@s.whatsapp.net"); ok {
		t.Fatal("picture still cached after delete")
	}
}
