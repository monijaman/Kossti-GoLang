package contact

import (
	"kossti/internal/domain/repository"
	"net/http"
	"strings"
)

// RegisterRoutes registers all contact routes
func RegisterRoutes(mux *http.ServeMux, repo repository.ContactRepository) {
	handler := NewContactHandler(repo)

	// Public route - Contact form submission
	mux.HandleFunc("/api/contacts", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost || r.Method == http.MethodOptions {
			handler.CreateContact(w, r)
		} else {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
	})

	// Admin routes - Contact management
	mux.HandleFunc("/admin/contacts/stats", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			handler.GetContactStats(w, r)
		} else {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
	})

	mux.HandleFunc("/admin/contacts/", func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/admin/contacts/")

		// Check if it's a status update endpoint
		if strings.Contains(path, "/status") {
			if r.Method == http.MethodPut {
				handler.UpdateContactStatus(w, r)
			} else {
				http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			}
			return
		}

		// Check if it's a specific contact (has ID in path)
		if path != "" && path != "/" {
			switch r.Method {
			case http.MethodGet:
				handler.GetContactByID(w, r)
			case http.MethodDelete:
				handler.DeleteContact(w, r)
			default:
				http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			}
			return
		}

		// List all contacts
		if r.Method == http.MethodGet {
			handler.GetAllContacts(w, r)
		} else {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
	})

	mux.HandleFunc("/admin/contacts", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			handler.GetAllContacts(w, r)
		} else {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
	})
}
