package flow

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/nyaruka/goflow/flows"
	"github.com/nyaruka/goflow/utils"
	"github.com/nyaruka/goflow/utils/uuids"
	"github.com/nyaruka/mailroom/goflow"
	"github.com/nyaruka/mailroom/models"
	"github.com/nyaruka/mailroom/web"

	"github.com/Masterminds/semver"
	"github.com/jmoiron/sqlx"
	"github.com/pkg/errors"
)

func init() {
	web.RegisterJSONRoute(http.MethodPost, "/mr/flow/migrate", web.RequireAuthToken(handleMigrate))
	web.RegisterJSONRoute(http.MethodPost, "/mr/flow/inspect", web.RequireAuthToken(handleInspect))
	web.RegisterJSONRoute(http.MethodPost, "/mr/flow/clone", web.RequireAuthToken(handleClone))
}

// Migrates a legacy flow to the new flow definition specification
//
//   {
//     "flow": {"uuid": "468621a8-32e6-4cd2-afc1-04416f7151f0", "action_sets": [], ...},
//     "to_version": "13.0.0"
//   }
//
type migrateRequest struct {
	Flow      json.RawMessage `json:"flow" validate:"required"`
	ToVersion *semver.Version `json:"to_version"`
}

func handleMigrate(ctx context.Context, s *web.Server, r *http.Request) (interface{}, int, error) {
	request := &migrateRequest{}
	if err := utils.UnmarshalAndValidateWithLimit(r.Body, request, web.MaxRequestBytes); err != nil {
		return errors.Wrapf(err, "request failed validation"), http.StatusBadRequest, nil
	}

	// do a JSON to JSON migration of the definition
	migrated, err := goflow.MigrateDefinition(request.Flow, request.ToVersion)
	if err != nil {
		return errors.Wrapf(err, "unable to migrate flow"), http.StatusUnprocessableEntity, nil
	}

	// try to read result to check that it's valid
	_, err = goflow.ReadFlow(migrated)
	if err != nil {
		return errors.Wrapf(err, "unable to read migrated flow"), http.StatusUnprocessableEntity, nil
	}

	return migrated, http.StatusOK, nil
}

// Inspects a flow, and returns metadata including the possible results generated by the flow,
// and dependencies in the flow. If `validate_with_org_id` is specified then the cloned flow
// will be validated against the assets of that org.
//
//   {
//     "flow": { "uuid": "468621a8-32e6-4cd2-afc1-04416f7151f0", "nodes": [...]},
//     "validate_with_org_id": 1
//   }
//
type inspectRequest struct {
	Flow              json.RawMessage `json:"flow" validate:"required"`
	ValidateWithOrgID models.OrgID    `json:"validate_with_org_id"`
}

func handleInspect(ctx context.Context, s *web.Server, r *http.Request) (interface{}, int, error) {
	request := &inspectRequest{}
	if err := utils.UnmarshalAndValidateWithLimit(r.Body, request, web.MaxRequestBytes); err != nil {
		return errors.Wrapf(err, "request failed validation"), http.StatusBadRequest, nil
	}

	flow, err := goflow.ReadFlow(request.Flow)
	if err != nil {
		return errors.Wrapf(err, "unable to read flow"), http.StatusUnprocessableEntity, nil
	}

	// if we have an org ID, do asset validation
	if request.ValidateWithOrgID != models.NilOrgID {
		result, status, err := checkDependencies(s.CTX, s.DB, request.ValidateWithOrgID, flow)
		if result != nil || err != nil {
			return result, status, err
		}
	}

	return flow.Inspect(), http.StatusOK, nil
}

// Clones a flow, replacing all UUIDs with either the given mapping or new random UUIDs.
// If `validate_with_org_id` is specified then the cloned flow will be validated against
// the assets of that org.
//
//   {
//     "dependency_mapping": {
//       "4ee4189e-0c06-4b00-b54f-5621329de947": "db31d23f-65b8-4518-b0f6-45638bfbbbf2",
//       "723e62d8-a544-448f-8590-1dfd0fccfcd4": "f1fd861c-9e75-4376-a829-dcf76db6e721"
//     },
//     "flow": { "uuid": "468621a8-32e6-4cd2-afc1-04416f7151f0", "nodes": [...]},
//     "validate_with_org_id": 1
//   }
//
type cloneRequest struct {
	DependencyMapping map[uuids.UUID]uuids.UUID `json:"dependency_mapping"`
	Flow              json.RawMessage           `json:"flow" validate:"required"`
	ValidateWithOrgID models.OrgID              `json:"validate_with_org_id"`
}

func handleClone(ctx context.Context, s *web.Server, r *http.Request) (interface{}, int, error) {
	request := &cloneRequest{}
	if err := utils.UnmarshalAndValidateWithLimit(r.Body, request, web.MaxRequestBytes); err != nil {
		return errors.Wrapf(err, "request failed validation"), http.StatusBadRequest, nil
	}

	// try to clone the flow definition
	cloneJSON, err := goflow.CloneDefinition(request.Flow, request.DependencyMapping)
	if err != nil {
		return errors.Wrapf(err, "unable to read flow"), http.StatusUnprocessableEntity, nil
	}

	// if we have an org ID, do asset validation on the new clone
	if request.ValidateWithOrgID != models.NilOrgID {
		clone, err := goflow.ReadFlow(cloneJSON)
		if err != nil {
			return errors.Wrapf(err, "unable to clone flow"), http.StatusUnprocessableEntity, nil
		}

		result, status, err := checkDependencies(s.CTX, s.DB, request.ValidateWithOrgID, clone)
		if result != nil || err != nil {
			return result, status, err
		}
	}

	return cloneJSON, http.StatusOK, nil
}

func checkDependencies(ctx context.Context, db *sqlx.DB, orgID models.OrgID, flow flows.Flow) (interface{}, int, error) {
	org, err := models.NewOrgAssets(ctx, db, orgID, nil)
	if err != nil {
		return nil, 0, err
	}

	sa, err := models.NewSessionAssets(org)
	if err != nil {
		return nil, 0, err
	}

	if err := flow.CheckDependencies(sa, nil); err != nil {
		return errors.Wrapf(err, "flow failed validation"), http.StatusUnprocessableEntity, nil
	}

	return nil, 0, nil
}
