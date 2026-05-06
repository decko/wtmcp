package main

import (
	"fmt"
	"os"

	"github.com/LeGambiArt/wtmcp/pkg/handler"
)

var (
	Version   = "dev"
	BuildDate = "unknown"
)

var plug *handler.Plugin

func main() {
	plug = handler.New()

	plug.OnInit(initConfig)

	plug.Handle("bugzilla_search", toolSearch)
	plug.Handle("bugzilla_get_bugs", toolGetBugs)
	plug.Handle("bugzilla_get_comments", toolGetComments)
	plug.Handle("bugzilla_get_history", toolGetHistory)
	plug.Handle("bugzilla_get_attachments", toolGetAttachments)
	plug.Handle("bugzilla_download_attachment", toolDownloadAttachment)
	plug.Handle("bugzilla_whoami", toolWhoami)
	plug.Handle("bugzilla_get_user", toolGetUser)
	plug.Handle("bugzilla_get_products", toolGetProducts)
	plug.Handle("bugzilla_get_fields", toolGetFields)
	plug.Handle("bugzilla_flush_cache", toolFlushCache)

	plug.Handle("bugzilla_create_bug", toolCreateBug)
	plug.Handle("bugzilla_update_bug", toolUpdateBug)
	plug.Handle("bugzilla_add_comment", toolAddComment)
	plug.Handle("bugzilla_add_attachment", toolAddAttachment)
	plug.Handle("bugzilla_update_attachment", toolUpdateAttachment)
	plug.Handle("bugzilla_mark_duplicate", toolMarkDuplicate)

	if err := plug.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "handler: %v\n", err)
		os.Exit(1)
	}
}
