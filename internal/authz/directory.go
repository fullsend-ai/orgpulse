package authz

import "strings"

// Directory returns the Rover groups for a person. user is a Kerberos
// username, an email address, or an email local-part. A nil or unknown user
// returns no groups.
type Directory interface {
	Groups(user string) []string
}

// MapDirectory is an in-memory directory keyed by lowercase username or email.
type MapDirectory map[string][]string

func (m MapDirectory) Groups(user string) []string {
	if len(m) == 0 {
		return nil
	}
	groups := m[strings.ToLower(strings.TrimSpace(user))]
	if len(groups) == 0 {
		return nil
	}
	out := make([]string, len(groups))
	copy(out, groups)
	return out
}

type unionDirectory []Directory

// Union merges directories. Later directories add groups the earlier ones
// did not already return. Nil directories are skipped.
func Union(dirs ...Directory) Directory {
	var out unionDirectory
	for _, dir := range dirs {
		if dir != nil {
			out = append(out, dir)
		}
	}
	if len(out) == 0 {
		return MapDirectory(nil)
	}
	return out
}

func (u unionDirectory) Groups(user string) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, dir := range u {
		for _, group := range dir.Groups(user) {
			group = strings.TrimSpace(group)
			if group == "" {
				continue
			}
			key := strings.ToLower(group)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			out = append(out, group)
		}
	}
	return out
}
