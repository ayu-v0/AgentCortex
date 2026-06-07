package cli

import "github.com/ayu-v0/agent-cortex/internal/model"

type Conversation struct {
	systemPrompt string
	messages     []model.Message
}

func NewConversation(systemPrompt string) *Conversation {
	conversation := &Conversation{}
	conversation.Clear(systemPrompt)
	return conversation
}

func (c *Conversation) Messages() []model.Message {
	messages := make([]model.Message, len(c.messages))
	copy(messages, c.messages)
	return messages
}

func (c *Conversation) MessagesWithUser(question string) []model.Message {
	messages := c.Messages()
	messages = append(messages, model.Message{
		Role:    model.RoleUser,
		Content: question,
	})
	return messages
}

func (c *Conversation) AddTurn(question, answer string) {
	c.messages = append(c.messages,
		model.Message{Role: model.RoleUser, Content: question},
		model.Message{Role: model.RoleAssistant, Content: answer},
	)
}

func (c *Conversation) Clear(systemPrompt string) {
	c.systemPrompt = systemPrompt
	c.messages = c.messages[:0]
	if systemPrompt != "" {
		c.messages = append(c.messages, model.Message{
			Role:    model.RoleSystem,
			Content: systemPrompt,
		})
	}
}
