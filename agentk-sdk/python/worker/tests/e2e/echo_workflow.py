from temporalio import workflow

from agentk.worker import report_progress

WORKFLOW_TYPE = "e2e.python_echo"


@workflow.defn(name=WORKFLOW_TYPE)
class EchoPipeline:
    @workflow.run
    async def run(self, args: dict[str, str]) -> dict[str, str]:
        value = args["value"]
        await report_progress(
            "received",
            "Input received",
            {"value": value},
        )
        return {"echoed": value}
