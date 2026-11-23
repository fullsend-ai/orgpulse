package authz

import (
	"encoding/csv"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// CSVDirectory reads Rover group membership from a CSV file or a directory of
// them. A Rover export is one group per file and has no group column; the
// group name is the filename without .csv. A file that already has a group
// column uses that column. The path is re-read when a file's size or
// modification time changes, or when a file is added or removed. A missing
// path, an unreadable directory, or a file that cannot be parsed yields no
// groups from that file.
type CSVDirectory struct {
	path string

	mu     sync.Mutex
	stamp  string
	loaded bool
	users  map[string][]string
}

// OpenCSV returns a directory backed by path. An empty path yields no groups.
func OpenCSV(path string) *CSVDirectory {
	return &CSVDirectory{path: strings.TrimSpace(path)}
}

func (d *CSVDirectory) Groups(user string) []string {
	if d == nil || d.path == "" {
		return nil
	}
	users := d.snapshot()
	groups := users[strings.ToLower(strings.TrimSpace(user))]
	if len(groups) == 0 {
		return nil
	}
	out := make([]string, len(groups))
	copy(out, groups)
	return out
}

func (d *CSVDirectory) snapshot() map[string][]string {
	stamp, err := membershipStamp(d.path)
	d.mu.Lock()
	defer d.mu.Unlock()
	if err != nil {
		if !d.loaded || d.users == nil {
			slog.Error("rover membership csv unreadable", "path", d.path, "err", err)
		}
		d.loaded = true
		d.users = map[string][]string{}
		d.stamp = ""
		return d.users
	}
	if d.loaded && stamp == d.stamp {
		return d.users
	}
	d.users = loadMembership(d.path)
	d.stamp = stamp
	d.loaded = true
	return d.users
}

func membershipStamp(path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return fileStamp(info), nil
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	for _, entry := range entries {
		if entry.IsDir() || groupFromFilename(entry.Name()) == "" {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return "", err
		}
		b.WriteString(entry.Name())
		b.WriteByte('\n')
		b.WriteString(fileStamp(info))
		b.WriteByte('\n')
	}
	return b.String(), nil
}

func fileStamp(info os.FileInfo) string {
	return info.ModTime().UTC().Format(time.RFC3339Nano) + ":" + strconv.FormatInt(info.Size(), 10)
}

func loadMembership(path string) map[string][]string {
	info, err := os.Stat(path)
	if err != nil {
		slog.Error("rover membership csv unreadable", "path", path, "err", err)
		return map[string][]string{}
	}
	if !info.IsDir() {
		return loadMembershipFile(path)
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		slog.Error("rover membership csv unreadable", "path", path, "err", err)
		return map[string][]string{}
	}
	users := map[string][]string{}
	for _, entry := range entries {
		if entry.IsDir() || groupFromFilename(entry.Name()) == "" {
			continue
		}
		mergeMembership(users, loadMembershipFile(filepath.Join(path, entry.Name())))
	}
	return users
}

func loadMembershipFile(path string) map[string][]string {
	users, err := parseMembershipFile(path)
	if err != nil {
		slog.Error("rover membership csv rejected", "path", path, "err", err)
		return map[string][]string{}
	}
	return users
}

func groupFromFilename(name string) string {
	base := filepath.Base(name)
	if base == "." || base == "" || strings.HasPrefix(base, ".") {
		return ""
	}
	if len(base) < 5 || !strings.EqualFold(base[len(base)-4:], ".csv") {
		return ""
	}
	return strings.TrimSpace(base[:len(base)-4])
}

func parseMembershipFile(path string) (map[string][]string, error) {
	f, err := os.Open(path) // #nosec G304 -- path is the configured membership file, not request input
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return parseMembership(f, groupFromFilename(path))
}

func parseMembership(r io.Reader, fallbackGroup string) (map[string][]string, error) {
	reader := csv.NewReader(r)
	reader.FieldsPerRecord = -1
	reader.TrimLeadingSpace = true
	header, err := reader.Read()
	if err != nil {
		return nil, err
	}
	groupCol, memberCol, emailCol := membershipColumns(header)
	if memberCol < 0 && emailCol < 0 {
		return nil, errors.New("membership csv needs a member or email column")
	}
	if groupCol < 0 && strings.TrimSpace(fallbackGroup) == "" {
		return nil, errors.New("membership csv needs a group column or a .csv filename")
	}
	users := map[string][]string{}
	for {
		rec, err := reader.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		group := fallbackGroup
		if groupCol >= 0 {
			group = field(rec, groupCol)
		}
		if group == "" {
			continue
		}
		addMembership(users, field(rec, memberCol), group)
		addMembership(users, field(rec, emailCol), group)
		if email := field(rec, emailCol); email != "" {
			if local, _, ok := strings.Cut(email, "@"); ok {
				addMembership(users, local, group)
			}
		}
	}
	return users, nil
}

func membershipColumns(header []string) (group, member, email int) {
	group, member, email = -1, -1, -1
	for i, raw := range header {
		name := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(raw, "\ufeff")))
		switch name {
		case "group", "group_name", "rover_group":
			if group < 0 {
				group = i
			}
		case "member", "uid", "username":
			if member < 0 {
				member = i
			}
		case "email", "mail":
			if email < 0 {
				email = i
			}
		}
	}
	return group, member, email
}

func field(rec []string, idx int) string {
	if idx < 0 || idx >= len(rec) {
		return ""
	}
	return strings.TrimSpace(rec[idx])
}

func mergeMembership(dst, src map[string][]string) {
	for person, groups := range src {
		for _, group := range groups {
			addMembership(dst, person, group)
		}
	}
}

func addMembership(users map[string][]string, person, group string) {
	person = strings.ToLower(strings.TrimSpace(person))
	group = strings.TrimSpace(group)
	if person == "" || group == "" {
		return
	}
	for _, existing := range users[person] {
		if strings.EqualFold(existing, group) {
			return
		}
	}
	users[person] = append(users[person], group)
}
