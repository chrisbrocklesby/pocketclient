![pocketclient](pocketclient.png)
# PocketClient (Go SDK)
A minimal, typed Go SDK for [PocketBase](https://pocketbase.io).

## Install

```sh
go get github.com/chrisbrocklesby/pocketclient
```

## Usage

### Create a client

```go
client := pocketclient.New("https://your-pocketbase.example.com")
```

### Auth

```go
// Authenticate against any collection
var user User
err := client.AuthPassword("users", "user@example.com", "password", &user)

// Authenticate as superuser
err := client.AuthPassword("_superusers", "admin@example.com", "password", nil)
```

The token is stored on the client automatically and sent with every subsequent request. If the token expires and a `401` is returned, the client will **automatically re-authenticate** and retry once. Disable this with:

```go
client.DisableAutoReauth()
```

Use a pre-existing token:

```go
client := pocketclient.New("https://your-pocketbase.example.com").WithToken("your-token")
```

### CRUD

```go
// Create
var record Post
err := client.Create("posts", PostInput{Title: "Hello"}, &record)

// View
var record Post
err := client.View("posts", "RECORD_ID", &record)

// Update
var record Post
err := client.Update("posts", "RECORD_ID", map[string]any{"title": "Updated"}, &record)

// List
var result pocketclient.Response[Post]
err := client.List("posts", &result, pocketclient.Query{
    "page":    1,
    "perPage": 50,
    "sort":    "-created",
    "filter":  `status = "published"`,
})

// Delete
err := client.Delete("posts", "RECORD_ID", nil)
```

### File uploads

Use `pocketclient.File` in your input struct when writing. File fields return as `string` or `[]string` when reading.

```go
type PostInput struct {
    Title string            `json:"title"`
    Cover pocketclient.File `json:"cover,omitempty"`
}

type Post struct {
    ID    string `json:"id"`
    Title string `json:"title"`
    Cover []string `json:"cover"` // filenames returned by PocketBase
}

err := client.Create("posts", PostInput{
    Title: "Hello",
    Cover: pocketclient.File{Name: "cover.png", Data: imgBytes},
}, &created)
```

Multiple files:

```go
type PostInput struct {
    Images []pocketclient.File `json:"images,omitempty"`
}
```

### Error handling

```go
err := client.View("posts", id, &post)
if err != nil {
    // Check for any API error
    if e, ok := pocketclient.IsError(err); ok {
        fmt.Println(e.Status, e.Body)
    }

    // Check for a specific status
    if _, ok := pocketclient.IsError(err, 404); ok {
        fmt.Println("not found")
    }

    // Check for multiple statuses
    if e, ok := pocketclient.IsError(err, 401, 403); ok {
        fmt.Println("auth error:", e.Status)
    }
}
```

### Raw requests

```go
// Simple
var health map[string]any
err := client.Raw("GET", "/api/health", nil, &health)

// With context
err := client.RawCtx(ctx, "GET", "/api/health", nil, &health)

// With custom headers
err := client.RawWithHeaders(ctx, "POST", "/api/custom", body, &out, pocketclient.Headers{
    "X-Custom-Header": "value",
})
```

## Response type

`pocketclient.Response[T]` maps PocketBase's paginated list response:

```go
type Response[T any] struct {
    Page       int
    PerPage    int
    TotalItems int
    TotalPages int
    Items      []T
}
```

## License

[MIT](LICENSE)
