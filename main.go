package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/labstack/echo"
	"github.com/labstack/echo/middleware"
)

func logRequestForDebug(c echo.Context, body any) {
	r := c.Request()
	rec := map[string]any{
		"datetime": time.Now().Format(time.RFC3339),
		"remote":   c.RealIP(),
		"method":   r.Method,
		"path":     r.URL.Path,
		"headers":  r.Header,
		"body":     body,
	}

	f, err := os.OpenFile("/request.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()

	json.NewEncoder(f).Encode(rec)
}

type Handler struct {
	Hostname string
}

func (h *Handler) RegisterRoutes(e *echo.Echo) {
	e.GET("/.well-known/nodeinfo", h.GetNodeInfo)
	e.GET("/.well-known/host-meta", h.GetHostMeta)
	e.GET("/.well-known/webfinger", h.GetWebFinger)
	e.GET("/@:username", h.GetUser)
	e.GET("/@:username/icon.png", h.GetIcon)
	e.POST("/@:username/inbox", h.PostInbox)
	e.GET("/@:username/outbox", h.GetOutbox)
	e.GET("/@:username/followers", h.GetFollowers)
	e.GET("/@:username/following", h.GetFollowing)
}

type XRD struct {
	Link []XRDLink `xml:"Link"`
}

type XRDLink struct {
	Rel      string `xml:"rel,attr"`
	Type     string `xml:"type,attr"`
	Template string `xml:"template,attr"`
}

func (h *Handler) GetNodeInfo(c echo.Context) error {
	return c.JSON(200, map[string]any{
		"version": "2.1",
		"software": map[string]string{
			"name":    "activitypub-sandbox",
			"version": "0.0.1",
		},
		"protocols": []string{
			"activitypub",
		},
		"usage": map[string]any{
			"users": map[string]int{
				"total": 1,
			},
		},
	})
}

func (h *Handler) GetHostMeta(c echo.Context) error {
	xrd := XRD{
		Link: []XRDLink{{
			Rel:      "lrdd",
			Type:     "application/xrd+xml",
			Template: fmt.Sprintf("https://%s/.well-known/webfinger?resource={uri}", c.Request().Host),
		}},
	}
	return c.XMLPretty(200, xrd, "  ")
}

func (h *Handler) GetWebFinger(c echo.Context) error {
	xs := strings.SplitN(c.QueryParam("resource"), "@", 2)
	if len(xs) == 2 && xs[1] != h.Hostname {
		return c.JSON(404, map[string]string{
			"error": "not found",
		})
	}

	username := xs[0]
	if strings.HasPrefix(username, "acct:") {
		username = username[len("acct:"):]
	}
	if username[0] == '@' {
		username = username[1:]
	}

	return c.JSON(200, map[string]any{
		"subject": fmt.Sprintf("acct:%s@%s", username, h.Hostname),
		"aliases": []string{
			fmt.Sprintf("https://%s/@%s", c.Request().Host, username),
		},
		"links": []map[string]string{
			{
				"rel":  "http://webfinger.net/rel/profile-page",
				"type": "text/html",
				"href": fmt.Sprintf("https://%s/@%s", c.Request().Host, username),
			},
			{
				"rel":  "self",
				"type": "application/activity+json",
				"href": fmt.Sprintf("https://%s/@%s", c.Request().Host, username),
			},
		},
	})
}

func (h *Handler) GetUser(c echo.Context) error {
	accepts := strings.Split(c.Request().Header.Get("Accept"), ",")

	for _, accept := range accepts {
		if strings.TrimSpace(accept) == "application/activity+json" {
			return h.GetUserActor(c)
		}
	}
	return h.GetUserPage(c)
}

func (h *Handler) GetIcon(c echo.Context) error {
	return c.File("public/icon.png")
}

func (h *Handler) GetUserPage(c echo.Context) error {
	username := c.Param("username")

	return c.HTML(200, fmt.Sprintf(`<h1>@%s</h1>not implemented yet.`, username))
}

func (h *Handler) GetUserActor(c echo.Context) error {
	username := c.Param("username")

	return c.JSON(200, map[string]any{
		"@context": []string{
			"https://www.w3.org/ns/activitystreams",
			"https://w3id.org/security/v1",
		},
		"id":                fmt.Sprintf("https://%s/@%s", h.Hostname, username),
		"type":              "Person",
		"name":              "DEBUG",
		"preferredUsername": username,
		"summary":           "<p>デバッグ用ニセアカウント。</p>",
		"published":         "2023-08-14T20:38:00+09:00",
		"icon": map[string]string{
			"type":      "Image",
			"mediaType": "image/png",
			"url":       fmt.Sprintf("https://%s/@%s/icon.png", h.Hostname, username),
		},
		"url":       fmt.Sprintf("https://%s/@%s", c.Request().Host, username),
		"inbox":     fmt.Sprintf("https://%s/@%s/inbox", c.Request().Host, username),
		"outbox":    fmt.Sprintf("https://%s/@%s/outbox", c.Request().Host, username),
		"followers": fmt.Sprintf("https://%s/@%s/followers", c.Request().Host, username),
		"following": fmt.Sprintf("https://%s/@%s/following", c.Request().Host, username),
		"publicKey": map[string]string{
			"id":           fmt.Sprintf("https://%s/@%s#main-key", c.Request().Host, username),
			"owner":        fmt.Sprintf("https://%s/@%s", c.Request().Host, username),
			"publicKeyPem": "",
		},
	})
}

func (h *Handler) PostInbox(c echo.Context) error {
	var request map[string]any
	if err := json.NewDecoder(c.Request().Body).Decode(&request); err != nil {
		return c.JSON(400, map[string]string{
			"error": "invalid request",
		})
	}

	logRequestForDebug(c, request)

	switch request["type"] {
	case "Follow":
		return h.PostInboxFollow(c, request)
	case "Undo":
		return h.PostInboxUndo(c, request)
	default:
		return c.JSON(400, map[string]string{
			"error": fmt.Sprintf("unsupported type: %q", request["type"]),
		})
	}
}

func (h *Handler) PostInboxFollow(c echo.Context, request map[string]any) error {
	username := c.Param("username")

	var accept bytes.Buffer
	if err := json.NewEncoder(&accept).Encode(map[string]any{
		"@context": "https://www.w3.org/ns/activitystreams",
		"id":       fmt.Sprintf("https://%s/@%s#follow", h.Hostname, username),
		"type":     "Accept",
		"actor":    fmt.Sprintf("https://%s/@%s", h.Hostname, username),
		"object":   request,
	}); err != nil {
		return c.JSON(500, map[string]string{
			"error": "internal server error",
		})
	}

	req, err := http.NewRequest("POST", request["actor"].(string), &accept)
	if err != nil {
		c.Logger().Printf("failed to prepare follow accept message: %s", err)
		return c.JSON(500, map[string]string{
			"error": "internal server error",
		})
	}

	req.Header.Set("Content-Type", "application/activity+json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		c.Logger().Printf("failed to send follow accept message: %s", err)
		return c.JSON(500, map[string]string{
			"error": "internal server error",
		})
	}

	if resp.StatusCode != 200 {
		c.Logger().Printf("follow accept message has denied: %s", err)
		return c.JSON(500, map[string]string{
			"error": "internal server error",
		})
	}

	return c.JSON(200, map[string]string{
		"status": "accepted",
	})
}

func (h *Handler) PostInboxUndo(c echo.Context, request map[string]any) error {
	return c.JSON(200, map[string]string{
		"status": "accepted",
	})
}

func (h *Handler) GetOutbox(c echo.Context) error {
	username := c.Param("username")

	page := c.QueryParam("page")
	if page == "" {
		return c.JSON(200, map[string]any{
			"@context":   "https://www.w3.org/ns/activitystreams",
			"id":         fmt.Sprintf("https://%s/@%s", h.Hostname, username),
			"type":       "OrderedCollection",
			"totalItems": 1,
			"first":      fmt.Sprintf("https://%s/@%s?page=0", h.Hostname, username),
			"last":       fmt.Sprintf("https://%s/@%s?page=0", h.Hostname, username),
		})
	} else {
		return c.JSON(200, map[string]any{
			"@context": "https://www.w3.org/ns/activitystreams",
			"id":       fmt.Sprintf("https://%s/@%s/outbox?page=0", h.Hostname, username),
			"type":     "OrderedCollectionPage",
			"partOf":   fmt.Sprintf("https://%s/@%s/outbox", h.Hostname, username),
			"orderedItems": []map[string]any{{
				"id":        fmt.Sprintf("https://%s/@%s/posts/12345", h.Hostname, username),
				"type":      "Create",
				"published": "2023-08-13T11:32:00Z",
				"actor":     fmt.Sprintf("https://%s/@%s", h.Hostname, username),
				"to": []string{
					"https://www.w3.org/ns/activitystreams#Public",
				},
				"cc": []string{
					fmt.Sprintf("https://%s/@%s/followers", h.Hostname, username),
				},
				"object": map[string]any{
					"id":           fmt.Sprintf("https://%s/@%s/posts/12345", h.Hostname, username),
					"type":         "Note",
					"published":    "2023-08-13T11:32:00Z",
					"attributedTo": fmt.Sprintf("https://%s/@%s", h.Hostname, username),
					"to": []string{
						"https://www.w3.org/ns/activitystreams#Public",
					},
					"cc": []string{
						fmt.Sprintf("https://%s/@%s/followers", h.Hostname, username),
					},
					"content": "Hello, world!",
				},
			}},
		})
	}
}

func (h *Handler) GetFollowers(c echo.Context) error {
	username := c.Param("username")
	page := c.QueryParam("page")

	if page == "" {
		return c.JSON(200, map[string]any{
			"@context":   "https://www.w3.org/ns/activitystreams",
			"id":         fmt.Sprintf("https://%s/@%s/followers", h.Hostname, username),
			"type":       "OrderedCollection",
			"totalItems": 314159265,
			"first":      fmt.Sprintf("https://%s/@%s/followers?page=0", h.Hostname, username),
		})
	} else {
		return c.JSON(200, map[string]any{
			"@context": "https://www.w3.org/ns/activitystreams",
			"id":       fmt.Sprintf("https://%s/@%s/followers?page=0", h.Hostname, username),
			"type":     "OrderedCollectionPage",
			"partOf":   fmt.Sprintf("https://%s/@%s/followers", h.Hostname, username),
			"orderedItems": []string{
				"https://mstdn.jp/users/macrat",
			},
			"next": fmt.Sprintf("https://%s/@%s/followers?page=1", h.Hostname, username),
		})
	}
}

func (h *Handler) GetFollowing(c echo.Context) error {
	username := c.Param("username")
	page := c.QueryParam("page")

	if page == "" {
		return c.JSON(200, map[string]any{
			"@context":   "https://www.w3.org/ns/activitystreams",
			"id":         fmt.Sprintf("https://%s/@%s/following", h.Hostname, username),
			"type":       "OrderedCollection",
			"totalItems": 1,
			"first":      fmt.Sprintf("https://%s/@%s/following?page=0", h.Hostname, username),
		})
	} else {
		return c.JSON(200, map[string]any{
			"@context": "https://www.w3.org/ns/activitystreams",
			"id":       fmt.Sprintf("https://%s/@%s/following?page=0", h.Hostname, username),
			"type":     "OrderedCollectionPage",
			"partOf":   fmt.Sprintf("https://%s/@%s/following", h.Hostname, username),
			"orderedItems": []string{
				"https://mstdn.jp/users/macrat",
			},
			"next": fmt.Sprintf("https://%s/@%s/following?page=1", h.Hostname, username),
		})
	}
}

func main() {
	e := echo.New()
	e.Use(middleware.Logger())
	h := &Handler{
		Hostname: "oxyfern.blanktar.jp",
	}
	h.RegisterRoutes(e)
	e.Logger.Fatal(e.Start(":8000"))
}
