package contact

import (
	"encoding/json"
	"kossti/internal/domain/entities"
	"kossti/internal/domain/repository"
	"net/http"
	"strconv"
	"strings"
)

// Request/Response structures
type CreateContactRequest struct {
	Name    string `json:"name"`
	Email   string `json:"email"`
	Subject string `json:"subject"`
	Message string `json:"message"`
}

type UpdateContactStatusRequest struct {
	Status    string `json:"status"`
	AdminNote string `json:"admin_note,omitempty"`
}

type ContactResponse struct {
	ID        uint   `json:"id"`
	Name      string `json:"name"`
	Email     string `json:"email"`
	Subject   string `json:"subject"`
	Message   string `json:"message"`
	IPAddress string `json:"ip_address"`
	UserAgent string `json:"user_agent"`
	Status    string `json:"status"`
	AdminNote string `json:"admin_note,omitempty"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

type ContactListResponse struct {
	Data       []ContactResponse `json:"data"`
	Total      int64             `json:"total"`
	Page       int               `json:"page"`
	Limit      int               `json:"limit"`
	TotalPages int               `json:"total_pages"`
	Message    string            `json:"message,omitempty"`
}

type ContactStatsResponse struct {
	Total    int64 `json:"total"`
	New      int64 `json:"new"`
	Read     int64 `json:"read"`
	Replied  int64 `json:"replied"`
	Archived int64 `json:"archived"`
}

// convertContactToResponse converts entity to response format
func convertContactToResponse(c *entities.Contact) ContactResponse {
	return ContactResponse{
		ID:        c.ID,
		Name:      c.Name,
		Email:     c.Email,
		Subject:   c.Subject,
		Message:   c.Message,
		IPAddress: c.IPAddress,
		UserAgent: c.UserAgent,
		Status:    c.Status,
		AdminNote: c.AdminNote,
		CreatedAt: c.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
		UpdatedAt: c.UpdatedAt.Format("2006-01-02T15:04:05Z07:00"),
	}
}

// ContactHandler handles HTTP requests for contact operations
type ContactHandler struct {
	repo repository.ContactRepository
}

// NewContactHandler creates a new contact handler
func NewContactHandler(repo repository.ContactRepository) *ContactHandler {
	return &ContactHandler{
		repo: repo,
	}
}

// CreateContact handles POST /api/contacts (public endpoint)
func (h *ContactHandler) CreateContact(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}

	var req CreateContactRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "Invalid request body"})
		return
	}

	// Validate required fields
	if req.Name == "" || req.Email == "" || req.Subject == "" || req.Message == "" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "Name, email, subject, and message are required"})
		return
	}

	// Get client IP and user agent
	ipAddress := r.RemoteAddr
	if forwarded := r.Header.Get("X-Forwarded-For"); forwarded != "" {
		ipAddress = strings.Split(forwarded, ",")[0]
	}
	userAgent := r.Header.Get("User-Agent")

	// Create contact entity
	contact := &entities.Contact{
		Name:      req.Name,
		Email:     req.Email,
		Subject:   req.Subject,
		Message:   req.Message,
		IPAddress: ipAddress,
		UserAgent: userAgent,
		Status:    "new",
	}

	// Save to database
	created, err := h.repo.Create(r.Context(), contact)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "Failed to create contact"})
		return
	}

	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"message": "Contact form submitted successfully",
		"data":    convertContactToResponse(created),
	})
}

// GetAllContacts handles GET /admin/contacts (admin endpoint)
func (h *ContactHandler) GetAllContacts(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	// Parse query parameters
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}

	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit < 1 {
		limit = 20
	}

	status := r.URL.Query().Get("status")

	offset := (page - 1) * limit

	var contacts []*entities.Contact
	var total int64
	var err error

	if status != "" {
		contacts, total, err = h.repo.GetByStatus(r.Context(), status, limit, offset)
	} else {
		contacts, total, err = h.repo.GetAll(r.Context(), limit, offset)
	}

	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "Failed to retrieve contacts"})
		return
	}

	responses := make([]ContactResponse, len(contacts))
	for i, contact := range contacts {
		responses[i] = convertContactToResponse(contact)
	}

	totalPages := int(total) / limit
	if int(total)%limit != 0 {
		totalPages++
	}

	json.NewEncoder(w).Encode(ContactListResponse{
		Data:       responses,
		Total:      total,
		Page:       page,
		Limit:      limit,
		TotalPages: totalPages,
	})
}

// GetContactByID handles GET /admin/contacts/{id}
func (h *ContactHandler) GetContactByID(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	// Extract ID from URL path
	path := strings.TrimPrefix(r.URL.Path, "/admin/contacts/")
	idStr := strings.Split(path, "/")[0]
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "Invalid contact ID"})
		return
	}

	contact, err := h.repo.GetByID(r.Context(), uint(id))
	if err != nil {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"error": "Contact not found"})
		return
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"data": convertContactToResponse(contact),
	})
}

// UpdateContactStatus handles PUT /admin/contacts/{id}/status
func (h *ContactHandler) UpdateContactStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	// Extract ID from URL path
	path := strings.TrimPrefix(r.URL.Path, "/admin/contacts/")
	parts := strings.Split(path, "/")
	if len(parts) < 2 {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "Invalid URL"})
		return
	}

	id, err := strconv.ParseUint(parts[0], 10, 32)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "Invalid contact ID"})
		return
	}

	var req UpdateContactStatusRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "Invalid request body"})
		return
	}

	if req.Status == "" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "Status is required"})
		return
	}

	// Validate status
	validStatuses := map[string]bool{"new": true, "read": true, "replied": true, "archived": true}
	if !validStatuses[req.Status] {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "Invalid status. Must be: new, read, replied, or archived"})
		return
	}

	if err := h.repo.UpdateStatus(r.Context(), uint(id), req.Status, req.AdminNote); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "Failed to update contact status"})
		return
	}

	json.NewEncoder(w).Encode(map[string]string{
		"message": "Contact status updated successfully",
	})
}

// DeleteContact handles DELETE /admin/contacts/{id}
func (h *ContactHandler) DeleteContact(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	// Extract ID from URL path
	path := strings.TrimPrefix(r.URL.Path, "/admin/contacts/")
	idStr := strings.Split(path, "/")[0]
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "Invalid contact ID"})
		return
	}

	if err := h.repo.Delete(r.Context(), uint(id)); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "Failed to delete contact"})
		return
	}

	json.NewEncoder(w).Encode(map[string]string{
		"message": "Contact deleted successfully",
	})
}

// GetContactStats handles GET /admin/contacts/stats
func (h *ContactHandler) GetContactStats(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	total, _ := h.repo.Count(r.Context())
	newCount, _ := h.repo.CountByStatus(r.Context(), "new")
	readCount, _ := h.repo.CountByStatus(r.Context(), "read")
	repliedCount, _ := h.repo.CountByStatus(r.Context(), "replied")
	archivedCount, _ := h.repo.CountByStatus(r.Context(), "archived")

	json.NewEncoder(w).Encode(ContactStatsResponse{
		Total:    total,
		New:      newCount,
		Read:     readCount,
		Replied:  repliedCount,
		Archived: archivedCount,
	})
}
