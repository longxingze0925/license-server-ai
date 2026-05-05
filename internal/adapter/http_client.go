package adapter

import (
	"net/http"
	"time"
)

var adapterHTTPClient = &http.Client{Timeout: 120 * time.Second}
