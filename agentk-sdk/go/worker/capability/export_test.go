package capability

// Exposed for external test packages, which cannot import invocation from
// inside package capability without an import cycle.
const InvokeWorkflowType = invokeWorkflowType

type InvokeInput = invokeInput
