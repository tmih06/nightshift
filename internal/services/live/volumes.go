package live

import (
	"context"

	v1 "github.com/tmih06/nightshift/api/v1"
	"github.com/tmih06/nightshift/internal/services/cluster"
)

// Volumes keeps what checkouts leave behind.
type Volumes struct {
	fleet *Fleet
}

// Volumes lists every volume, with the sandbox that mounts it.
func (v *Volumes) Volumes(ctx context.Context) ([]v1.Volume, error) {
	kept, err := call(ctx, cluster.ListVolumes)
	if err != nil {
		return nil, err
	}
	snap, err := v.fleet.refresh(ctx)
	if err != nil {
		return nil, err
	}
	mounted := map[string]string{}
	for i := range snap.boxes {
		if b := &snap.boxes[i]; b.Volume != "" {
			mounted[b.Volume] = b.Name
		}
	}
	out := make([]v1.Volume, 0, len(kept))
	for _, k := range kept {
		out = append(out, v1.Volume{
			Name: k.Name, Target: targetOf(k.For), Cloned: k.Cloned,
			Sandbox: mounted[k.Name], CreatedAt: k.Created.UTC(),
		})
	}
	return out, nil
}

// DeleteVolume deletes a volume and the clone on it.
func (*Volumes) DeleteVolume(ctx context.Context, name string) error {
	_, err := call(ctx, func() (struct{}, error) { return struct{}{}, cluster.DeleteVolume(name) })
	return err
}

// DeleteSandbox ends a sandbox a checkout started, and the volume too when
// asked. A claimed sandbox is not this: it goes with its claim.
func (v *Volumes) DeleteSandbox(ctx context.Context, name string, deleteVolume bool) error {
	snap, err := v.fleet.refresh(ctx)
	if err != nil {
		return err
	}
	var volume string
	for i := range snap.boxes {
		if b := &snap.boxes[i]; b.Name == name && b.Volume != "" {
			volume = b.Volume
		}
	}
	if volume == "" {
		return &notFoundError{what: "sandbox " + name + " with a volume"}
	}
	if _, err := call(ctx, func() (struct{}, error) { return struct{}{}, cluster.DeleteSandbox(name) }); err != nil {
		return err
	}
	if !deleteVolume {
		return nil
	}
	return v.DeleteVolume(ctx, volume)
}
