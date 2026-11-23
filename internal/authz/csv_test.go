package authz

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseMembershipColumns(t *testing.T) {
	dir := OpenCSV(filepath.Join("testdata", "rover"))
	got := dir.Groups("Alice")
	if len(got) != 1 || got[0] != "rhai-org" {
		t.Fatalf("username groups = %v", got)
	}
	if groups := dir.Groups("cara@example.com"); len(groups) != 1 || groups[0] != "t1-group" {
		t.Fatalf("email groups = %v", groups)
	}
	if groups := dir.Groups("bob"); len(groups) != 1 || groups[0] != "p1-group" {
		t.Fatalf("local-part groups = %v", groups)
	}
	if groups := dir.Groups("nobody"); groups != nil {
		t.Fatalf("unknown user groups = %v", groups)
	}
}

func TestParseMembershipReloadAndReject(t *testing.T) {
	path := filepath.Join(t.TempDir(), "groups.csv")
	if err := os.WriteFile(path, []byte("group,uid\nops,pat\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	dir := OpenCSV(path)
	if groups := dir.Groups("pat"); len(groups) != 1 || groups[0] != "ops" {
		t.Fatalf("groups = %v", groups)
	}
	if err := os.WriteFile(path, []byte("not,a,membership,file\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if groups := dir.Groups("pat"); groups != nil {
		t.Fatalf("rejected file still returned %v", groups)
	}
}

func TestRoverFilenameIsTheGroup(t *testing.T) {
	path := filepath.Join(t.TempDir(), "testaisdlc.csv")
	body := "name,uid,email,role\nFran Example,fexample,fexample@example.com,member\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	groups := OpenCSV(path).Groups("fexample@example.com")
	if len(groups) != 1 || groups[0] != "testaisdlc" {
		t.Fatalf("groups = %v", groups)
	}
}

func TestGroupColumnOverridesFilename(t *testing.T) {
	path := filepath.Join(t.TempDir(), "other.csv")
	if err := os.WriteFile(path, []byte("group,uid\nrhai-org,alice\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	groups := OpenCSV(path).Groups("alice")
	if len(groups) != 1 || groups[0] != "rhai-org" {
		t.Fatalf("groups = %v", groups)
	}
}

func TestDirectorySkipsUnusableFiles(t *testing.T) {
	dir := t.TempDir()
	body := "name,uid,email,role\nAlice Example,alice,alice@example.com,member\n"
	if err := os.WriteFile(filepath.Join(dir, "rhai-org.csv"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "broken.csv"), []byte("not,a,membership,file\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".hidden.csv"), []byte("name,uid,email\nHidden,hidden,hidden@example.com\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("name,uid,email\nNote,note,note@example.com\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	groups := OpenCSV(dir).Groups("alice")
	if len(groups) != 1 || groups[0] != "rhai-org" {
		t.Fatalf("alice groups = %v", groups)
	}
	if groups := OpenCSV(dir).Groups("hidden"); groups != nil {
		t.Fatalf("hidden file groups = %v", groups)
	}
	if groups := OpenCSV(dir).Groups("note"); groups != nil {
		t.Fatalf("text file groups = %v", groups)
	}
}

func TestOpenCSVMissingFile(t *testing.T) {
	dir := OpenCSV(filepath.Join(t.TempDir(), "missing.csv"))
	if groups := dir.Groups("alice"); groups != nil {
		t.Fatalf("missing file groups = %v", groups)
	}
	if groups := OpenCSV("").Groups("alice"); groups != nil {
		t.Fatalf("empty path groups = %v", groups)
	}
}
