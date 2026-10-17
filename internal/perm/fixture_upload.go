package perm

import "github.com/google/uuid"

// RoleUpload sets a role's upload override (nil inherits).
func (f *Fixture) RoleUpload(role uuid.UUID, mb *int) *Fixture {
	r := f.s.roles[role]
	r.MaxUploadMB = mb
	f.s.roles[role] = r
	return f
}

// GlobalUpload sets the Owner's global upload limit (nil = the ceiling).
func (f *Fixture) GlobalUpload(mb *int) *Fixture {
	f.s.globalUploadMB = mb
	return f
}
