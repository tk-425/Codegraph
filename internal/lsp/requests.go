package lsp

// LSP Request/Response types for initialization and common requests

// InitializeParams sent to server during initialization
type InitializeParams struct {
	ProcessID    int                `json:"processId"`
	RootURI      string             `json:"rootUri"`
	Capabilities ClientCapabilities `json:"capabilities"`
}

// ClientCapabilities describes client capabilities
type ClientCapabilities struct {
	TextDocument TextDocumentClientCapabilities `json:"textDocument,omitempty"`
	Workspace    WorkspaceClientCapabilities    `json:"workspace,omitempty"`
}

// TextDocumentClientCapabilities for text document features
type TextDocumentClientCapabilities struct {
	DocumentSymbol DocumentSymbolClientCapabilities `json:"documentSymbol,omitempty"`
	CallHierarchy  CallHierarchyClientCapabilities  `json:"callHierarchy,omitempty"`
	TypeHierarchy  TypeHierarchyClientCapabilities  `json:"typeHierarchy,omitempty"`
}

// DocumentSymbolClientCapabilities for document symbols
type DocumentSymbolClientCapabilities struct {
	HierarchicalDocumentSymbolSupport bool `json:"hierarchicalDocumentSymbolSupport,omitempty"`
}

// CallHierarchyClientCapabilities for call hierarchy
type CallHierarchyClientCapabilities struct {
	DynamicRegistration bool `json:"dynamicRegistration,omitempty"`
}

// TypeHierarchyClientCapabilities for type hierarchy
type TypeHierarchyClientCapabilities struct {
	DynamicRegistration bool `json:"dynamicRegistration,omitempty"`
}

// WorkspaceClientCapabilities for workspace features
type WorkspaceClientCapabilities struct {
	Symbol WorkspaceSymbolClientCapabilities `json:"symbol,omitempty"`
}

// WorkspaceSymbolClientCapabilities for workspace symbols
type WorkspaceSymbolClientCapabilities struct {
	DynamicRegistration bool `json:"dynamicRegistration,omitempty"`
}

// InitializeResult returned by server after initialization
type InitializeResult struct {
	Capabilities ServerCapabilities `json:"capabilities"`
}

// ServerCapabilities describes what the server can do
// Note: Many fields use `any` because LSP servers can return either
// a boolean (true/false) or an options object for each capability
type ServerCapabilities struct {
	TextDocumentSync           any `json:"textDocumentSync,omitempty"`
	DocumentSymbolProvider     any `json:"documentSymbolProvider,omitempty"`
	WorkspaceSymbolProvider    any `json:"workspaceSymbolProvider,omitempty"`
	DefinitionProvider         any `json:"definitionProvider,omitempty"`
	ReferencesProvider         any `json:"referencesProvider,omitempty"`
	ImplementationProvider     any `json:"implementationProvider,omitempty"`
	CallHierarchyProvider      any `json:"callHierarchyProvider,omitempty"`
	TypeHierarchyProvider      any `json:"typeHierarchyProvider,omitempty"`
}

// DocumentSymbolParams for textDocument/documentSymbol request
type DocumentSymbolParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
}

// WorkspaceSymbolParams for workspace/symbol request
type WorkspaceSymbolParams struct {
	Query string `json:"query"`
}

// CallHierarchyPrepareParams for callHierarchy/prepare
type CallHierarchyPrepareParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
	Position     Position               `json:"position"`
}

// CallHierarchyIncomingCallsParams for callHierarchy/incomingCalls
type CallHierarchyIncomingCallsParams struct {
	Item CallHierarchyItem `json:"item"`
}

// CallHierarchyOutgoingCallsParams for callHierarchy/outgoingCalls
type CallHierarchyOutgoingCallsParams struct {
	Item CallHierarchyItem `json:"item"`
}

// TypeHierarchyPrepareParams for typeHierarchy/prepare
type TypeHierarchyPrepareParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
	Position     Position               `json:"position"`
}

// TypeHierarchySupertypesParams for typeHierarchy/supertypes
type TypeHierarchySupertypesParams struct {
	Item TypeHierarchyItem `json:"item"`
}

// TypeHierarchySubtypesParams for typeHierarchy/subtypes
type TypeHierarchySubtypesParams struct {
	Item TypeHierarchyItem `json:"item"`
}

// ImplementationParams for textDocument/implementation
type ImplementationParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
	Position     Position               `json:"position"`
}

// DefaultClientCapabilities returns capabilities we advertise to servers
func DefaultClientCapabilities() ClientCapabilities {
	return ClientCapabilities{
		TextDocument: TextDocumentClientCapabilities{
			DocumentSymbol: DocumentSymbolClientCapabilities{
				HierarchicalDocumentSymbolSupport: true,
			},
			CallHierarchy: CallHierarchyClientCapabilities{
				DynamicRegistration: false,
			},
			TypeHierarchy: TypeHierarchyClientCapabilities{
				DynamicRegistration: false,
			},
		},
		Workspace: WorkspaceClientCapabilities{
			Symbol: WorkspaceSymbolClientCapabilities{
				DynamicRegistration: false,
			},
		},
	}
}
