package main

import (
	"encoding/json"

	"github.com/LeGambiArt/wtmcp/pkg/handler"
)

func toolCreateBug(_ /* params */, _ /* config */ json.RawMessage) (any, error) {
	return nil, &handler.Error{Code: "not_implemented", Message: "bugzilla_create_bug not yet implemented"}
}

func toolUpdateBug(_ /* params */, _ /* config */ json.RawMessage) (any, error) {
	return nil, &handler.Error{Code: "not_implemented", Message: "bugzilla_update_bug not yet implemented"}
}

func toolAddComment(_ /* params */, _ /* config */ json.RawMessage) (any, error) {
	return nil, &handler.Error{Code: "not_implemented", Message: "bugzilla_add_comment not yet implemented"}
}

func toolAddAttachment(_ /* params */, _ /* config */ json.RawMessage) (any, error) {
	return nil, &handler.Error{Code: "not_implemented", Message: "bugzilla_add_attachment not yet implemented"}
}

func toolUpdateAttachment(_ /* params */, _ /* config */ json.RawMessage) (any, error) {
	return nil, &handler.Error{Code: "not_implemented", Message: "bugzilla_update_attachment not yet implemented"}
}

func toolMarkDuplicate(_ /* params */, _ /* config */ json.RawMessage) (any, error) {
	return nil, &handler.Error{Code: "not_implemented", Message: "bugzilla_mark_duplicate not yet implemented"}
}
