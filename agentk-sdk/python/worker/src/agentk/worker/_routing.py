from typing import Any, TypeVar, cast

from temporalio import workflow
from temporalio.workflow import (
    ChildWorkflowCancellationType,
    ParentClosePolicy,
)

_INVOKE_WORKFLOW_TYPE = "agentk.capability.invoke.v1"
_CORE_TASK_QUEUE = "agentk"

ResultT = TypeVar("ResultT")


async def invoke_capability(
    capability_id: str,
    input: Any,
    *,
    result_type: type[ResultT],
) -> ResultT:
    """Ask AgentK Core to resolve and call a capability from a Workflow.

    AgentK selects the registered Temporal workflow or activity for
    capability_id. This function waits for the target to complete and
    deserializes its result as result_type. Workflow targets receive a
    cancellation request when their parent closes; Activity targets wait for
    cancellation to complete.

    Args:
        capability_id: The globally unique ID of the capability to call.
        input: Input passed to the resolved workflow or activity.
        result_type: The Python type used to deserialize the result.

    Returns:
        The deserialized capability result.
    """
    result = await workflow.execute_child_workflow(
        _INVOKE_WORKFLOW_TYPE,
        {"capabilityId": capability_id, "input": input},
        task_queue=_CORE_TASK_QUEUE,
        result_type=result_type,
        cancellation_type=ChildWorkflowCancellationType.WAIT_CANCELLATION_COMPLETED,
        parent_close_policy=ParentClosePolicy.REQUEST_CANCEL,
    )
    return cast(ResultT, result)
