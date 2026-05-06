package main

import (
	"encoding/json"
	"fmt"

	"github.com/LeGambiArt/wtmcp/pkg/handler"
)

func toolSearch(_ /* params */, _ /* config */ json.RawMessage) (any, error) {
	return nil, &handler.Error{Code: "not_implemented", Message: "bugzilla_search not yet implemented"}
}

func toolGetBugs(_ /* params */, _ /* config */ json.RawMessage) (any, error) {
	return nil, &handler.Error{Code: "not_implemented", Message: "bugzilla_get_bugs not yet implemented"}
}

func toolGetComments(_ /* params */, _ /* config */ json.RawMessage) (any, error) {
	return nil, &handler.Error{Code: "not_implemented", Message: "bugzilla_get_comments not yet implemented"}
}

func toolGetHistory(_ /* params */, _ /* config */ json.RawMessage) (any, error) {
	return nil, &handler.Error{Code: "not_implemented", Message: "bugzilla_get_history not yet implemented"}
}

func toolGetAttachments(_ /* params */, _ /* config */ json.RawMessage) (any, error) {
	return nil, &handler.Error{Code: "not_implemented", Message: "bugzilla_get_attachments not yet implemented"}
}

func toolDownloadAttachment(_ /* params */, _ /* config */ json.RawMessage) (any, error) {
	return nil, &handler.Error{Code: "not_implemented", Message: "bugzilla_download_attachment not yet implemented"}
}

func toolWhoami(_ /* params */, _ /* config */ json.RawMessage) (any, error) {
	return nil, &handler.Error{Code: "not_implemented", Message: "bugzilla_whoami not yet implemented"}
}

func toolGetUser(_ /* params */, _ /* config */ json.RawMessage) (any, error) {
	return nil, &handler.Error{Code: "not_implemented", Message: "bugzilla_get_user not yet implemented"}
}

func toolGetProducts(_ /* params */, _ /* config */ json.RawMessage) (any, error) {
	return nil, &handler.Error{Code: "not_implemented", Message: "bugzilla_get_products not yet implemented"}
}

func toolGetFields(_ /* params */, _ /* config */ json.RawMessage) (any, error) {
	return nil, &handler.Error{Code: "not_implemented", Message: "bugzilla_get_fields not yet implemented"}
}

func toolFlushCache(_, _ json.RawMessage) (any, error) {
	count, err := plug.CacheFlush()
	if err != nil {
		return nil, fmt.Errorf("flush cache: %w", err)
	}
	return map[string]any{"flushed": count}, nil
}
