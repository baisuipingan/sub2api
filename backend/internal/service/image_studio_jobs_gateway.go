package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"strconv"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
)

func (s *StudioJobService) buildStudioGatewayRequest(ctx context.Context, claim StudioJobClaim) (*http.Request, error) {
	key, err := s.keys.GetByID(ctx, claim.APIKeyID)
	if err != nil || key == nil || key.UserID != claim.UserID || key.Key == "" {
		return nil, ErrStudioJobUnavailable
	}
	spec := claim.Spec
	if s.gateway == nil || key.Group == nil {
		return nil, ErrStudioJobRouteChanged
	}
	models, err := s.gateway.ListImageStudioModels(ctx, key.Group)
	if err != nil {
		return nil, ErrStudioJobUnavailable
	}
	if !studioProfilesCompatible(studioCurrentProfiles(spec, models), spec.AdmissionProfiles) {
		return nil, ErrStudioJobRouteChanged
	}
	var images []struct {
		data []byte
		mime string
	}
	var total int64
	for _, id := range spec.RequestInputImageIDs {
		data, mime, err := s.gallery.ReadAsset(ctx, claim.UserID, id)
		if err != nil {
			return nil, err
		}
		total += int64(len(data))
		if total > studioJobMaxInputBytes {
			return nil, ErrStudioJobInvalid
		}
		images = append(images, struct {
			data []byte
			mime string
		}{data, mime})
	}
	var mask []byte
	var maskMIME string
	if spec.MaskImageID != "" {
		mask, maskMIME, err = s.gallery.ReadAsset(ctx, claim.UserID, spec.MaskImageID)
		if err != nil {
			return nil, err
		}
		total += int64(len(mask))
		if total > studioJobMaxInputBytes {
			return nil, ErrStudioJobInvalid
		}
	}
	var body []byte
	path, contentType := "", "application/json"
	params := make(map[string]any)
	if err := json.Unmarshal(spec.Params, &params); err != nil {
		return nil, ErrStudioJobInvalid
	}
	switch spec.Protocol {
	case "openai":
		if len(images) == 0 {
			path = "/v1/images/generations"
			params["model"], params["prompt"], params["n"] = spec.Model, spec.Prompt, 1
			body, err = json.Marshal(params)
		} else {
			path = "/v1/images/edits"
			var buffer bytes.Buffer
			form := multipart.NewWriter(&buffer)
			_ = form.WriteField("model", spec.Model)
			_ = form.WriteField("prompt", spec.Prompt)
			params["n"] = 1
			for name, value := range params {
				if value == nil || value == false || value == "" {
					continue
				}
				_ = form.WriteField(name, fmt.Sprint(value))
			}
			for i, img := range images {
				part, partErr := studioCreateImagePart(form, "image[]", "reference-"+strconv.Itoa(i+1)+studioImageExtension(img.mime), img.mime)
				if partErr != nil {
					return nil, partErr
				}
				if _, err = part.Write(img.data); err != nil {
					return nil, err
				}
			}
			if len(mask) != 0 {
				part, partErr := studioCreateImagePart(form, "mask", "mask"+studioImageExtension(maskMIME), maskMIME)
				if partErr != nil {
					return nil, partErr
				}
				if _, err = part.Write(mask); err != nil {
					return nil, err
				}
			}
			if err = form.Close(); err != nil {
				return nil, err
			}
			body, contentType = buffer.Bytes(), form.FormDataContentType()
		}
	case "grok":
		if len(images) == 0 {
			path = "/v1/images/generations"
		} else {
			path = "/v1/images/edits"
		}
		params["model"], params["prompt"], params["n"] = spec.Model, spec.Prompt, spec.ImageCount
		refs := make([]map[string]string, 0, len(images))
		for _, img := range images {
			refs = append(refs, map[string]string{"url": "data:" + img.mime + ";base64," + base64.StdEncoding.EncodeToString(img.data), "type": "image_url"})
		}
		if len(refs) == 1 {
			params["image"] = refs[0]
		} else if len(refs) > 1 {
			params["images"] = refs
		}
		body, err = json.Marshal(params)
	case "gemini":
		path = "/v1beta/models/" + url.PathEscape(spec.Model) + ":generateContent"
		parts := []map[string]any{{"text": spec.Prompt}}
		for _, img := range images {
			parts = append(parts, map[string]any{"inlineData": map[string]string{"mimeType": img.mime, "data": base64.StdEncoding.EncodeToString(img.data)}})
		}
		body, err = json.Marshal(map[string]any{"contents": []any{map[string]any{"role": "user", "parts": parts}}, "generationConfig": params["generationConfig"]})
	default:
		return nil, ErrStudioJobInvalid
	}
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > studioJobMaxInputBytes*2 {
		return nil, ErrStudioJobInvalid
	}
	request, err := http.NewRequestWithContext(context.WithValue(ctx, ctxkey.ClientRequestID, studioClientRequestID(claim)), http.MethodPost, path, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Authorization", "Bearer "+key.Key)
	request.Header.Set("Content-Type", contentType)
	request.Header.Set("User-Agent", "sub2api-image-studio-worker")
	request.RemoteAddr = "127.0.0.1:0"
	return request, nil
}

func studioImageExtension(mime string) string {
	switch mime {
	case "image/jpeg":
		return ".jpg"
	case "image/webp":
		return ".webp"
	case "image/gif":
		return ".gif"
	default:
		return ".png"
	}
}

func studioCurrentProfiles(spec StudioJobSpec, models []ImageStudioModel) []ImageStudioModelProfile {
	api, provider := "openai", "openai"
	if spec.Protocol == "grok" {
		provider = "xai"
	}
	if spec.Protocol == "gemini" {
		api, provider = "gemini", "google"
	}
	for _, model := range models {
		if model.ID != spec.Model || model.API != api {
			continue
		}
		var profiles []ImageStudioModelProfile
		for _, profile := range model.Profiles {
			allowed := profile.Provider == provider || profile.Provider == "unknown" ||
				(spec.Protocol == "openai" && profile.Provider == "google")
			if allowed {
				profiles = append(profiles, profile)
			}
		}
		return profiles
	}
	return nil
}

func studioProfilesCompatible(current, admitted []ImageStudioModelProfile) bool {
	if len(current) == 0 || len(admitted) == 0 {
		return false
	}
	for _, profile := range current {
		found := false
		for _, original := range admitted {
			if profile == original {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func studioCreateImagePart(form *multipart.Writer, field, filename, mime string) (io.Writer, error) {
	header := make(textproto.MIMEHeader)
	header.Set("Content-Disposition", fmt.Sprintf("form-data; name=%q; filename=%q", field, filename))
	header.Set("Content-Type", mime)
	return form.CreatePart(header)
}

func studioResponseImages(protocol string, response []byte) ([]map[string]json.RawMessage, error) {
	var images []map[string]json.RawMessage
	if protocol == "gemini" {
		var payload struct {
			Candidates []struct {
				Content struct {
					Parts []struct {
						Thought    bool `json:"thought"`
						InlineData *struct {
							MIMEType string `json:"mimeType"`
							Data     string `json:"data"`
						} `json:"inlineData"`
					} `json:"parts"`
				} `json:"content"`
			} `json:"candidates"`
		}
		if json.Unmarshal(response, &payload) != nil {
			return nil, ErrGalleryInvalid
		}
		for _, candidate := range payload.Candidates {
			for _, part := range candidate.Content.Parts {
				if part.Thought || part.InlineData == nil || !strings.HasPrefix(part.InlineData.MIMEType, "image/") {
					continue
				}
				item, _ := json.Marshal(part.InlineData.Data)
				images = append(images, map[string]json.RawMessage{"b64_json": item})
			}
		}
	} else {
		var payload struct {
			Data []map[string]json.RawMessage `json:"data"`
		}
		if json.Unmarshal(response, &payload) != nil {
			return nil, ErrGalleryInvalid
		}
		images = payload.Data
	}
	if len(images) == 0 || len(images) > 10 {
		return nil, ErrGalleryInvalid
	}
	return images, nil
}

func (s *StudioJobService) archiveStudioImages(ctx context.Context, claim StudioJobClaim, response []byte) ([]string, []string, error) {
	images, err := studioResponseImages(claim.Spec.Protocol, response)
	if err != nil {
		return nil, nil, err
	}
	uploader := NewImageResultUploader(nil, "", GalleryMaxImageBytes, nil)
	outputs, raws := make([]string, 0, len(images)), []string{}
	var source, mask []byte
	if claim.Spec.Postprocess == "mask" {
		source, _, err = s.gallery.ReadAsset(ctx, claim.UserID, claim.Spec.MaskTargetImageID)
		if err != nil {
			return nil, nil, err
		}
		mask, _, err = s.gallery.ReadAsset(ctx, claim.UserID, claim.Spec.MaskImageID)
		if err != nil {
			return nil, nil, err
		}
	}
	for index, item := range images {
		data, mime, err := s.studioImageBytes(ctx, claim, index, item, uploader)
		if err != nil {
			return nil, nil, err
		}
		if claim.Spec.Postprocess == "keyed" {
			rawID := studioOutputID(claim, index, "raw")
			if err := s.gallery.PutAcceptedJobAsset(ctx, claim.UserID, claim.TaskID, rawID, mime, data); err != nil {
				return nil, nil, err
			}
			raws = append(raws, rawID)
		}
		processed, processedMIME, err := ProcessStudioImage(ctx, data, source, mask, StudioPostprocessMode(claim.Spec.Postprocess), claim.Spec.OutputFormat, claim.Spec.OutputCompression)
		if err != nil {
			return nil, nil, err
		}
		outputID := studioOutputID(claim, index, "out")
		if err := s.gallery.PutAcceptedJobAsset(ctx, claim.UserID, claim.TaskID, outputID, processedMIME, processed); err != nil {
			return nil, nil, err
		}
		outputs = append(outputs, outputID)
	}
	return outputs, raws, nil
}

func studioOutputID(claim StudioJobClaim, index int, variant string) string {
	key := strconv.FormatInt(claim.UserID, 10) + ":" + claim.TaskID + ":" + strconv.Itoa(claim.Call.Ordinal) + ":" + strconv.Itoa(index) + ":" + variant
	digest := sha256.Sum256([]byte(key))
	return "job-" + hex.EncodeToString(digest[:16])
}
