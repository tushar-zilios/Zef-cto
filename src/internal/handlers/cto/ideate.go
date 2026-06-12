package cto

import (
	"encoding/json"
	"net/http"
	"time"

	"cto/src/internal/clients"
	"cto/src/internal/db"
	"cto/src/internal/utils"
)

type IdeateRequest struct {
	Message string `json:"message"`
	Model   string `json:"model"`
}

type IdeateResponse struct {
	Text string `json:"text"`
}

type IdeateMessage struct {
	MessageID string    `json:"message_id"`
	Sender    string    `json:"sender"`
	Text      string    `json:"text"`
	Model     string    `json:"model"`
	CreatedAt time.Time `json:"created_at"`
}

const ideateSystemPrompt = `You are an expert technology strategist and innovation consultant embedded in a CTO dashboard. Your role is to help CTOs and technical leaders brainstorm, ideate, and think through:

- New product features and technical directions
- Architecture decisions and trade-offs
- Technology stack choices and modernization strategies
- Engineering team structures and processes
- Innovation opportunities and competitive positioning
- Technical debt resolution strategies
- Build vs. buy decisions

Be concise yet thorough. Think in terms of business impact, technical feasibility, and engineering pragmatism. Use structured thinking when appropriate (pros/cons, options, recommendations). Always consider scalability, maintainability, and team capabilities.`

func IdeateHandler(w http.ResponseWriter, r *http.Request) {
	if !ctoDBRequired(w) {
		return
	}
	userID, _ := r.Context().Value("user_id").(string)

	var req IdeateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		utils.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	if req.Message == "" {
		utils.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "message is required"})
		return
	}

	model := req.Model
	if model == "" {
		model = "gemini-2.5-flash"
	}

	text, err := clients.GenerateContentWithModel(r.Context(), ideateSystemPrompt, req.Message, model)
	if err != nil {
		utils.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	if userID != "" {
		db.GetCTOPoolOrNil().Exec(r.Context(), `
			INSERT INTO public.cto_ideate_messages (user_id, sender, text, model)
			VALUES ($1, 'user', $2, $3), ($1, 'assistant', $4, $3)
		`, userID, req.Message, model, text)
	}

	utils.WriteJSON(w, http.StatusOK, IdeateResponse{Text: text})
}

func IdeateHistoryHandler(w http.ResponseWriter, r *http.Request) {
	if !ctoDBRequired(w) {
		return
	}
	userID, _ := r.Context().Value("user_id").(string)
	if userID == "" {
		utils.WriteJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	rows, err := db.GetCTOPoolOrNil().Query(r.Context(), `
		SELECT message_id, sender, text, model, created_at
		FROM public.cto_ideate_messages
		WHERE user_id = $1
		ORDER BY created_at ASC
	`, userID)
	if err != nil {
		utils.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer rows.Close()

	messages := []IdeateMessage{}
	for rows.Next() {
		var m IdeateMessage
		if err := rows.Scan(&m.MessageID, &m.Sender, &m.Text, &m.Model, &m.CreatedAt); err != nil {
			continue
		}
		messages = append(messages, m)
	}

	utils.WriteJSON(w, http.StatusOK, map[string]any{"messages": messages})
}

func IdeateClearHistoryHandler(w http.ResponseWriter, r *http.Request) {
	if !ctoDBRequired(w) {
		return
	}
	userID, _ := r.Context().Value("user_id").(string)
	if userID == "" {
		utils.WriteJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	db.GetCTOPoolOrNil().Exec(r.Context(), `DELETE FROM public.cto_ideate_messages WHERE user_id = $1`, userID)
	utils.WriteJSON(w, http.StatusOK, map[string]string{"status": "cleared"})
}
