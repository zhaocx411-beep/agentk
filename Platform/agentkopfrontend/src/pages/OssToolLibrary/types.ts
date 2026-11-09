export type OssToolCategory = '视频生成' | '视频编辑' | '视频增强' | '音频' | '教育';

export type OssToolForm = '完整应用' | 'Agent 项目' | '命令行工具' | '开发库' | '在线服务';

export type OssToolMaintenance = '活跃' | '稳定' | '待核实';

export interface OssTool {
  id: string;
  name: string;
  summary: string;
  capabilities: string;
  category: OssToolCategory;
  form: OssToolForm;
  repoUrl?: string;
  websiteUrl?: string;
  license: string;
  usage: string;
  risk: string;
  maintenance: OssToolMaintenance;
  tags: string[];
}

export interface OssToolFilters {
  keyword: string;
  category?: OssToolCategory;
  form?: OssToolForm;
  license?: string;
}
