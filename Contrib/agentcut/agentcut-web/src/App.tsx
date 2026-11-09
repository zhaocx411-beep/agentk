// 页面骨架：顶部栏 + 可调宽三栏 + 可调高 Timeline。布局比例保存在浏览器，
// 窄屏或需要精剪时可把更多空间分配给素材、预览或轨道。
import { useEffect } from "react";

import { ResizableEditorLayout } from "@/components/Layout/ResizableEditorLayout";
import { TopBar } from "@/components/TopBar/TopBar";
import { Toaster } from "@/components/ui/toaster";
import { resetAgentSync } from "@/store/agentSync";
import { useProjectStore } from "@/store/projectStore";
import { resetSubmitQueue } from "@/store/submitOps";

export default function App() {
  const loadProjects = useProjectStore((s) => s.loadProjects);

  useEffect(() => {
    void loadProjects();
  }, [loadProjects]);

  useEffect(() => {
    const cancelTimelineWork = () => {
      resetSubmitQueue("页面已离开，本地待提交修改已取消");
      resetAgentSync();
    };
    window.addEventListener("pagehide", cancelTimelineWork);
    return () => {
      window.removeEventListener("pagehide", cancelTimelineWork);
      cancelTimelineWork();
    };
  }, []);

  return (
    <div className="flex h-screen flex-col bg-[#F2F2F0] text-foreground">
      <TopBar />
      <ResizableEditorLayout />
      <Toaster />
    </div>
  );
}
