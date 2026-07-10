package bulkregistration

import "context"

// Store keeps bulk task state separately from the Proto-backed WA account
// store. Provider payloads and sensitive activation data never cross the RPC
// contract boundary.
type Store interface {
	CreateTask(context.Context, Task, []Item) (*Task, bool, error)
	GetActiveTask(context.Context) (*Task, error)
	GetTask(context.Context, string) (*Task, error)
	ListItems(context.Context, string) ([]Item, error)
	SaveTask(context.Context, Task) error
	SaveItem(context.Context, Item) error
	AppendEvent(context.Context, Event) error
}
