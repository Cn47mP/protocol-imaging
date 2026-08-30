package project

import (
	"errors"
	"fmt"
	"math"
	"path"
	"regexp"
	"strings"
	"time"
)

var stableIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]*$`)

var forbiddenCaptureFields = map[string]struct{}{
	"base_id":          {},
	"base_profile":     {},
	"base_profile_id":  {},
	"expected_width":   {},
	"expected_height":  {},
	"expected_rows":    {},
	"expected_columns": {},
	"map_width":        {},
	"map_height":       {},
	"map_size":         {},
	"profile_id":       {},
}

func (manifest Manifest) Validate() error {
	if manifest.Format != FormatName {
		return fmt.Errorf("format must be %q", FormatName)
	}
	if manifest.FormatVersion != FormatVersion {
		return fmt.Errorf("unsupported format_version %d", manifest.FormatVersion)
	}
	if err := validateStableID("project_id", manifest.ProjectID); err != nil {
		return err
	}
	if strings.TrimSpace(manifest.Title) == "" {
		return errors.New("title is required")
	}
	if err := validateUTCTime("created_at", manifest.CreatedAt); err != nil {
		return err
	}
	if err := validateUTCTime("updated_at", manifest.UpdatedAt); err != nil {
		return err
	}
	if manifest.UpdatedAt.Before(manifest.CreatedAt) {
		return errors.New("updated_at cannot precede created_at")
	}
	if strings.TrimSpace(manifest.GameVersion) == "" {
		return errors.New("game_version is required; use unknown when unavailable")
	}
	if manifest.Profile != nil {
		if err := validateStableID("profile.id", manifest.Profile.ID); err != nil {
			return err
		}
		if err := ValidateArchivePath(manifest.Profile.Snapshot); err != nil {
			return fmt.Errorf("profile.snapshot: %w", err)
		}
	}
	if err := manifest.Geometry.Validate(); err != nil {
		return fmt.Errorf("geometry: %w", err)
	}
	if err := manifest.CoordinateSystem.Validate(manifest.ProjectID); err != nil {
		return fmt.Errorf("coordinate_system: %w", err)
	}
	if !manifest.CaptureState.Valid() {
		return fmt.Errorf("unknown capture_state %q", manifest.CaptureState)
	}
	if manifest.CaptureState == CaptureComplete && manifest.Geometry.Status != GeometryObservedLocal {
		return errors.New("complete capture requires observed_local geometry")
	}
	if err := ValidateArchivePath(manifest.Capture); err != nil {
		return fmt.Errorf("capture: %w", err)
	}
	if captureStateRequiresCalibration(manifest.CaptureState) && manifest.ActiveCalibration == "" {
		return fmt.Errorf("capture_state %q requires active_calibration", manifest.CaptureState)
	}
	if manifest.ActiveCalibration != "" {
		if err := validateStableID("active_calibration", manifest.ActiveCalibration); err != nil {
			return err
		}
	}
	if captureStateRequiresPlan(manifest.CaptureState) && manifest.ActivePlan == "" {
		return fmt.Errorf("capture_state %q requires active_plan", manifest.CaptureState)
	}
	if manifest.ActivePlan != "" {
		if err := ValidateArchivePath(manifest.ActivePlan); err != nil {
			return fmt.Errorf("active_plan: %w", err)
		}
	}
	if manifest.ActiveVersion != "" {
		if err := validateStableID("active_version", manifest.ActiveVersion); err != nil {
			return err
		}
	}
	if manifest.CaptureState == CaptureComplete && manifest.ActiveVersion == "" {
		return errors.New("complete capture requires active_version")
	}
	if manifest.CaptureState == CaptureComplete && len(manifest.Layers) == 0 {
		return errors.New("complete capture requires at least one layer")
	}
	seenLayers := make(map[string]struct{}, len(manifest.Layers))
	for index, layer := range manifest.Layers {
		if err := validateStableID(fmt.Sprintf("layers[%d].id", index), layer.ID); err != nil {
			return err
		}
		if _, duplicate := seenLayers[layer.ID]; duplicate {
			return fmt.Errorf("duplicate layer id %q", layer.ID)
		}
		seenLayers[layer.ID] = struct{}{}
		if err := ValidateArchivePath(layer.Path); err != nil {
			return fmt.Errorf("layers[%d].path: %w", index, err)
		}
	}
	if manifest.Annotations != nil {
		if err := ValidateArchivePath(*manifest.Annotations); err != nil {
			return fmt.Errorf("annotations: %w", err)
		}
	}
	return nil
}

func (geometry GeometryDescriptor) Validate() error {
	if !geometry.Status.Valid() {
		return fmt.Errorf("unknown status %q", geometry.Status)
	}
	if geometry.Source != "dynamic_boundary_observation" {
		return fmt.Errorf("source must be dynamic_boundary_observation in format v1, got %q", geometry.Source)
	}
	if err := ValidateArchivePath(geometry.Observation); err != nil {
		return fmt.Errorf("observation: %w", err)
	}
	if geometry.CoordinateCompatibility != "session_local" {
		return fmt.Errorf("coordinate_compatibility must be session_local in format v1, got %q", geometry.CoordinateCompatibility)
	}
	return nil
}

func (coordinate CoordinateSystem) Validate(projectID string) error {
	expectedSpaceID := "session:" + projectID
	if coordinate.SpaceID != expectedSpaceID {
		return fmt.Errorf("space_id must be %q, got %q", expectedSpaceID, coordinate.SpaceID)
	}
	if coordinate.Unit != "reference_layer_pixel" {
		return fmt.Errorf("unit must be reference_layer_pixel, got %q", coordinate.Unit)
	}
	if coordinate.Axis != "x_right_y_down" {
		return fmt.Errorf("axis must be x_right_y_down, got %q", coordinate.Axis)
	}
	return nil
}

func (document CaptureDocument) Validate() error {
	if document.SchemaVersion != CaptureSchemaVersion {
		return fmt.Errorf("unsupported capture schema_version %d", document.SchemaVersion)
	}
	if err := rejectForbiddenCaptureFields("capture", document.Extra); err != nil {
		return err
	}
	if err := document.Request.Validate(); err != nil {
		return fmt.Errorf("request: %w", err)
	}
	if err := document.Environment.Validate(); err != nil {
		return fmt.Errorf("environment: %w", err)
	}
	if err := document.Limits.Validate(); err != nil {
		return fmt.Errorf("limits: %w", err)
	}
	seen := make(map[string]struct{}, len(document.Calibrations))
	for index, calibration := range document.Calibrations {
		if err := calibration.Validate(document.Environment.RawFrameSize); err != nil {
			return fmt.Errorf("calibrations[%d]: %w", index, err)
		}
		if _, duplicate := seen[calibration.ID]; duplicate {
			return fmt.Errorf("duplicate calibration id %q", calibration.ID)
		}
		seen[calibration.ID] = struct{}{}
	}
	return nil
}

func (request CaptureRequest) Validate() error {
	if request.Version <= 0 {
		return errors.New("version must be positive")
	}
	if err := validateUTCTime("frozen_at", request.FrozenAt); err != nil {
		return err
	}
	if err := validateStableID("imaging_mode", request.ImagingMode); err != nil {
		return err
	}
	if err := validateStableID("quality_level", request.QualityLevel); err != nil {
		return err
	}
	if err := request.TargetOverlap.Validate(); err != nil {
		return fmt.Errorf("target_overlap: %w", err)
	}
	if !request.BurstPolicy.Valid() {
		return fmt.Errorf("unknown burst_policy %q", request.BurstPolicy)
	}
	if !request.Diagnostics.Valid() {
		return fmt.Errorf("unknown diagnostics policy %q", request.Diagnostics)
	}
	return rejectForbiddenCaptureFields("request", request.Extra)
}

func (overlap Overlap) Validate() error {
	if !finite(overlap.Horizontal) || overlap.Horizontal <= 0 || overlap.Horizontal >= 1 {
		return fmt.Errorf("horizontal overlap must be within (0,1), got %g", overlap.Horizontal)
	}
	if !finite(overlap.Vertical) || overlap.Vertical <= 0 || overlap.Vertical >= 1 {
		return fmt.Errorf("vertical overlap must be within (0,1), got %g", overlap.Vertical)
	}
	return nil
}

func (environment EnvironmentFingerprint) Validate() error {
	if err := validateUTCTime("observed_at", environment.ObservedAt); err != nil {
		return err
	}
	if err := validateStableID("controller_kind", environment.ControllerKind); err != nil {
		return err
	}
	if err := environment.RawFrameSize.Validate(); err != nil {
		return fmt.Errorf("raw_frame_size: %w", err)
	}
	if err := environment.InputViewport.Validate(); err != nil {
		return fmt.Errorf("input_viewport: %w", err)
	}
	if err := environment.EffectiveCrop.Validate(); err != nil {
		return fmt.Errorf("effective_crop: %w", err)
	}
	if environment.EffectiveCrop.X < 0 || environment.EffectiveCrop.Y < 0 ||
		environment.EffectiveCrop.Right() > float64(environment.RawFrameSize.Width) ||
		environment.EffectiveCrop.Bottom() > float64(environment.RawFrameSize.Height) {
		return errors.New("effective_crop must stay within raw_frame_size")
	}
	if !finite(environment.DPIScale) || environment.DPIScale <= 0 || environment.DPIScale > 16 {
		return fmt.Errorf("dpi_scale must be within (0,16], got %g", environment.DPIScale)
	}
	if err := environment.Window.Validate(); err != nil {
		return fmt.Errorf("window: %w", err)
	}
	if strings.TrimSpace(environment.GameVersion) == "" {
		return errors.New("game_version is required; use unknown when unavailable")
	}
	return nil
}

func (dimensions PixelDimensions) Validate() error {
	if dimensions.Width <= 0 || dimensions.Height <= 0 {
		return fmt.Errorf("dimensions must be positive, got %dx%d", dimensions.Width, dimensions.Height)
	}
	if dimensions.Width > 1_000_000 || dimensions.Height > 1_000_000 {
		return fmt.Errorf("dimensions exceed defensive limit: %dx%d", dimensions.Width, dimensions.Height)
	}
	return nil
}

func (window WindowFingerprint) Validate() error {
	if strings.TrimSpace(window.ProcessName) == "" {
		return errors.New("process_name is required")
	}
	if strings.TrimSpace(window.ClassName) == "" {
		return errors.New("class_name is required")
	}
	if strings.TrimSpace(window.TitleHash) == "" {
		return errors.New("title_hash is required")
	}
	if err := window.ClientSize.Validate(); err != nil {
		return fmt.Errorf("client_size: %w", err)
	}
	return nil
}

func (calibration CalibrationRecord) Validate(rawSize PixelDimensions) error {
	if err := validateStableID("id", calibration.ID); err != nil {
		return err
	}
	if err := validateUTCTime("created_at", calibration.CreatedAt); err != nil {
		return err
	}
	if len(calibration.Actions) == 0 {
		return errors.New("actions must contain calibration evidence")
	}
	for index, action := range calibration.Actions {
		if err := action.Validate(); err != nil {
			return fmt.Errorf("actions[%d]: %w", index, err)
		}
	}
	if err := calibration.HorizontalMotion.Validate(); err != nil {
		return fmt.Errorf("horizontal_motion: %w", err)
	}
	if err := calibration.VerticalMotion.Validate(); err != nil {
		return fmt.Errorf("vertical_motion: %w", err)
	}
	if calibration.HorizontalMotion.Length() == 0 || calibration.VerticalMotion.Length() == 0 {
		return errors.New("horizontal_motion and vertical_motion must be non-zero")
	}
	if err := calibration.EffectiveViewport.Validate(); err != nil {
		return fmt.Errorf("effective_viewport: %w", err)
	}
	if calibration.EffectiveViewport.X < 0 || calibration.EffectiveViewport.Y < 0 ||
		calibration.EffectiveViewport.Right() > float64(rawSize.Width) ||
		calibration.EffectiveViewport.Bottom() > float64(rawSize.Height) {
		return errors.New("effective_viewport must stay within raw frame")
	}
	if err := calibration.InputToRaw.Validate(); err != nil {
		return fmt.Errorf("input_to_raw: %w", err)
	}
	if err := calibration.RawToSession.Validate(); err != nil {
		return fmt.Errorf("raw_to_session: %w", err)
	}
	if !finite(calibration.Confidence) || calibration.Confidence < 0 || calibration.Confidence > 1 {
		return fmt.Errorf("confidence must be within [0,1], got %g", calibration.Confidence)
	}
	if calibration.InvalidatedAt != nil {
		if err := validateUTCTime("invalidated_at", *calibration.InvalidatedAt); err != nil {
			return err
		}
		if calibration.InvalidatedAt.Before(calibration.CreatedAt) {
			return errors.New("invalidated_at cannot precede created_at")
		}
		if strings.TrimSpace(calibration.InvalidationReason) == "" {
			return errors.New("invalidation_reason is required when invalidated_at is set")
		}
	} else if calibration.InvalidationReason != "" {
		return errors.New("invalidated_at is required when invalidation_reason is set")
	}
	return nil
}

func (action CalibrationAction) Validate() error {
	if err := validateStableID("purpose", action.Purpose); err != nil {
		return err
	}
	if err := action.InputDelta.Validate(); err != nil {
		return fmt.Errorf("input_delta: %w", err)
	}
	if err := action.MeasuredRawDelta.Validate(); err != nil {
		return fmt.Errorf("measured_raw_delta: %w", err)
	}
	if action.InputDelta.Length() == 0 || action.MeasuredRawDelta.Length() == 0 {
		return errors.New("input_delta and measured_raw_delta must be non-zero")
	}
	if len(action.EvidenceIDs) == 0 {
		return errors.New("evidence_ids must not be empty")
	}
	for _, id := range action.EvidenceIDs {
		if err := validateStableID("evidence_id", id); err != nil {
			return err
		}
	}
	return nil
}

// ValidateBundle checks references that span manifest.json and capture.json.
func ValidateBundle(manifest Manifest, document CaptureDocument) error {
	if err := manifest.Validate(); err != nil {
		return fmt.Errorf("manifest: %w", err)
	}
	if err := document.Validate(); err != nil {
		return fmt.Errorf("capture: %w", err)
	}
	if manifest.GameVersion != "unknown" && document.Environment.GameVersion != "unknown" &&
		manifest.GameVersion != document.Environment.GameVersion {
		return fmt.Errorf("game_version mismatch: manifest=%q capture=%q", manifest.GameVersion, document.Environment.GameVersion)
	}
	if manifest.ActiveCalibration != "" {
		found := false
		for _, calibration := range document.Calibrations {
			if calibration.ID == manifest.ActiveCalibration {
				if calibration.InvalidatedAt != nil {
					return fmt.Errorf("active calibration %q is invalidated", calibration.ID)
				}
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("active calibration %q is missing from capture.json", manifest.ActiveCalibration)
		}
	}
	if document.Request.FrozenAt.After(manifest.UpdatedAt) {
		return errors.New("request frozen_at cannot be after manifest updated_at")
	}
	return nil
}

// ValidateArchivePath accepts only normalized, relative ZIP-style paths.
func ValidateArchivePath(value string) error {
	if value == "" {
		return errors.New("path is required")
	}
	if strings.ContainsRune(value, '\x00') {
		return errors.New("path contains NUL")
	}
	if strings.Contains(value, "\\") {
		return errors.New("path must use forward slashes")
	}
	if strings.Contains(value, ":") || strings.HasPrefix(value, "/") {
		return errors.New("absolute paths and URI schemes are forbidden")
	}
	if path.Clean(value) != value || value == "." {
		return errors.New("path must be normalized")
	}
	for _, segment := range strings.Split(value, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return errors.New("path contains an unsafe segment")
		}
	}
	return nil
}

func captureStateRequiresCalibration(state CaptureState) bool {
	switch state {
	case CaptureCapturing, CaptureRepairing, CaptureProcessing, CaptureComplete:
		return true
	default:
		return false
	}
}

func captureStateRequiresPlan(state CaptureState) bool {
	switch state {
	case CaptureCapturing, CaptureRepairing, CaptureProcessing, CaptureComplete:
		return true
	default:
		return false
	}
}

func rejectForbiddenCaptureFields(scope string, fields ExtraFields) error {
	for key := range fields {
		normalized := strings.ToLower(strings.TrimSpace(key))
		if _, forbidden := forbiddenCaptureFields[normalized]; forbidden {
			return fmt.Errorf("%s contains forbidden v1 field %q; base identity and expected geometry are later-version concerns", scope, key)
		}
	}
	return nil
}

func validateStableID(field, value string) error {
	if !stableIDPattern.MatchString(value) {
		return fmt.Errorf("%s must be a non-empty stable ASCII id, got %q", field, value)
	}
	return nil
}

func validateUTCTime(field string, value time.Time) error {
	if value.IsZero() {
		return fmt.Errorf("%s is required", field)
	}
	_, offset := value.Zone()
	if offset != 0 {
		return fmt.Errorf("%s must be UTC", field)
	}
	return nil
}

func finite(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}
