package main

import (
	"fmt"
	"log"
	"os"

	"github.com/chrisbrocklesby/pocketclient"
	"github.com/joho/godotenv"
)

type User struct {
	ID    string `json:"id"`
	Email string `json:"email"`
}

type Post struct {
	ID      string   `json:"id,omitempty"`
	Title   string   `json:"title"`
	Content string   `json:"content"`
	File    []string `json:"file"`  // filenames when reading
	Files   []string `json:"files"` // filenames when reading
}

type PostInput struct {
	Title   string              `json:"title"`
	Content string              `json:"content"`
	File    pocketclient.File   `json:"file,omitempty"`
	Files   []pocketclient.File `json:"files,omitempty"`
}

func main() {
	// --- LOAD ENV --- //
	err := godotenv.Load(".env")
	if err != nil {
		log.Println("Env file not loaded, using default environment variables.")
	}

	// --- INIT CLIENT --- //
	client := pocketclient.New(os.Getenv("baseURL"))

	// --- AUTH --- //
	var user User
	err = client.AuthPassword("_superusers", os.Getenv("identity"), os.Getenv("password"), &user)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("Logged in:", user.Email)

	// --- LOAD FILES --- //
	img1, err := os.ReadFile("./a.png")
	if err != nil {
		log.Fatal(err)
	}

	img2, err := os.ReadFile("./b.png")
	if err != nil {
		log.Fatal(err)
	}

	// --- CREATE --- //
	var created Post
	err = client.Create("posts", PostInput{
		Title:   "Hello",
		Content: "Post with files",
		File:    pocketclient.File{Name: "cover.png", Data: img1},
		Files: []pocketclient.File{
			{Name: "a.png", Data: img1},
			{Name: "b.png", Data: img2},
		},
	}, &created)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("Created:", created.ID)

	// --- VIEW --- //
	var post Post
	err = client.View("posts", created.ID, &post)
	if err != nil {
		if e, ok := pocketclient.IsError(err, 404); ok {
			fmt.Println("Not found:", e.Body)
			return
		}
		log.Fatal(err)
	}
	fmt.Println("Viewed:", post.Title, "| file:", post.File)

	// --- UPDATE --- //
	var updated Post
	err = client.Update("posts", created.ID, PostInput{
		Title:   "Updated title",
		Content: "Updated content",
		File:    pocketclient.File{Name: "updated.png", Data: img2},
	}, &updated)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("Updated:", updated.Title)

	// --- LIST --- //
	var posts pocketclient.Response[Post]
	err = client.List("posts", &posts, pocketclient.Query{
		"page":    1,
		"perPage": 50,
		"sort":    "-created",
	})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Page %d/%d — %d total\n", posts.Page, posts.TotalPages, posts.TotalItems)
	for _, p := range posts.Items {
		fmt.Println("-", p.ID, p.Title)
	}

	// --- DELETE --- //
	err = client.Delete("posts", created.ID, nil)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("Deleted:", created.ID)

	// --- RAW --- //
	var health map[string]any
	err = client.Raw("GET", "/api/health", nil, &health)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("Health:", health["code"])
}
