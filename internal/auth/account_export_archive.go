package auth

import (
	"archive/zip"
	"context"
	"fmt"
	"io"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/userdata"
)

// writeArchive collects the person's own data into a ZIP written to out.
// Channel names appear only for channels the person can still view. Each file
// is built when it is written, so profile and emoji images land just before
// their JSON and messages.json is not held in memory.
func (h *Handler) writeArchive(ctx context.Context, user uuid.UUID, out io.Writer) error {
	z := &zipFiles{}
	files, err := h.archiveFiles(ctx, user, z)
	if err != nil {
		return err
	}
	z.zw = zip.NewWriter(out)
	if err := writeReadme(z); err != nil {
		return err
	}
	cut, err := z.writeFiles(files)
	if err != nil {
		return err
	}
	if len(cut) > 0 {
		if err := z.add("truncated.json", map[string]any{"limit": exportMaxRows, "files": cut}); err != nil {
			return err
		}
	}
	return z.zw.Close()
}

func writeReadme(z *zipFiles) error {
	w, err := z.zw.Create("README.txt")
	if err != nil {
		return err
	}
	_, err = w.Write([]byte(exportReadme))
	return err
}

func (h *Handler) archiveFiles(ctx context.Context, user uuid.UUID, z *zipFiles) ([]userdata.File, error) {
	channels, err := h.viewableChannels(ctx, user)
	if err != nil {
		return nil, err
	}
	files := h.ownExportFiles(ctx, user, channels, z)
	more, err := h.personalData().Files(ctx, h.Sessions.DB, user, channels, exportLimits())
	if err != nil {
		return nil, err
	}
	files = append(files, more...)
	userdata.SortFiles(files)
	return files, nil
}

// ExportFileNames is the archive's data files in write order, without
// README.txt and without truncated.json.
func (h *Handler) ExportFileNames(ctx context.Context, user uuid.UUID) ([]string, error) {
	files, err := h.archiveFiles(ctx, user, &zipFiles{})
	if err != nil {
		return nil, err
	}
	names := make([]string, len(files))
	for i := range files {
		names[i] = files[i].Name
	}
	return names, nil
}

func exportLimits() userdata.Limits {
	return userdata.Limits{
		Page:         exportPage,
		MaxRows:      exportMaxRows,
		MaxMessages:  exportMaxMessages,
		MessageBytes: exportMessageBytes,
		MaxEdits:     exportMaxEdits,
	}
}

func (h *Handler) ownExportFiles(ctx context.Context, user uuid.UUID, channels userdata.Channels, z *zipFiles) []userdata.File {
	q := h.Sessions.DB
	return []userdata.File{
		plainFile("profile.json", userdata.FileProfile, func() (any, error) {
			return h.exportProfileWithImages(ctx, z, user)
		}),
		cappedFile("settings.json", userdata.FileSettings, func() (capped, error) {
			return exportSettings(ctx, q, user, channels)
		}),
		cappedFile("sessions.json", userdata.FileSessions, func() (capped, error) {
			return exportSessions(ctx, q, user)
		}),
		cappedFile("emoji.json", userdata.FileEmoji, func() (capped, error) {
			return h.exportEmoji(ctx, z, user)
		}),
		cappedFile("audit.json", userdata.FileAudit, func() (capped, error) {
			return exportAudit(ctx, q, user)
		}),
		plainFile("security.json", userdata.FileSecurity, func() (any, error) {
			return h.exportSecurity(ctx, user)
		}),
	}
}

func plainFile(name string, order int, load func() (any, error)) userdata.File {
	return userdata.File{
		Name: name, Order: order,
		Build: func() (any, bool, error) {
			v, err := load()
			return v, false, err
		},
	}
}

func cappedFile(name string, order int, load func() (capped, error)) userdata.File {
	return userdata.File{
		Name: name, Order: order,
		Build: func() (any, bool, error) {
			c, err := load()
			if err != nil {
				return nil, false, err
			}
			return c.value, c.truncated, nil
		},
	}
}

func (z *zipFiles) writeFiles(files []userdata.File) ([]string, error) {
	var cut []string
	for _, f := range files {
		name, err := z.writeFile(f)
		if err != nil {
			return nil, err
		}
		if name != "" {
			cut = append(cut, name)
		}
	}
	return cut, nil
}

func (z *zipFiles) writeFile(f userdata.File) (string, error) {
	if f.Stream != nil {
		return "", z.streamFile(f)
	}
	value, truncated, err := f.Build()
	if err != nil {
		return "", fmt.Errorf("%s: %w", f.Name, err)
	}
	if err := z.add(f.Name, value); err != nil {
		return "", err
	}
	if truncated {
		return f.Name, nil
	}
	return "", nil
}

func (z *zipFiles) streamFile(f userdata.File) error {
	w, err := z.zw.Create(f.Name)
	if err != nil {
		return err
	}
	if err := f.Stream(w); err != nil {
		return fmt.Errorf("%s: %w", f.Name, err)
	}
	return nil
}
