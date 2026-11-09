package discovery

import (
	"context"
	"errors"
	"fmt"

	"agentk/internal/capcatalog"
	"agentk/internal/temporalx"
)

type Activities struct {
	catalog capcatalog.Catalog
}

func NewActivities(catalog capcatalog.Catalog) *Activities {
	return &Activities{catalog: catalog}
}

func (a *Activities) RegisterManifest(
	ctx context.Context,
	input RegisterActivityInput,
) error {
	const op = "register capability manifest"
	if a.catalog == nil {
		return temporalx.ClassifyActivityError(op,
			temporalx.ConfigurationError(fmt.Errorf("capability catalog is required")))
	}
	manifest, err := capcatalog.DecodeManifestObject(input.Manifest)
	if err != nil {
		return temporalx.ClassifyActivityError(op,
			temporalx.InvalidActivityInput("%v", err))
	}
	manifest, _, err = capcatalog.NormalizeManifest(manifest)
	if err != nil {
		return temporalx.ClassifyActivityError(op,
			temporalx.InvalidActivityInput("%v", err))
	}
	if err := capcatalog.ValidateContribManifest(manifest); err != nil {
		return temporalx.ClassifyActivityError(op,
			temporalx.InvalidActivityInput("%v", err))
	}
	if _, err := a.catalog.Register(ctx, manifest); err != nil {
		if errors.Is(err, capcatalog.ErrRefConflict) {
			err = temporalx.InvalidActivityInput("%v", err)
		}
		return temporalx.ClassifyActivityError(op, err)
	}
	return nil
}
