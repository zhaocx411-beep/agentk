#!/usr/bin/env python3
"""Fix imports after module directory reorganizations."""
from __future__ import annotations

import os
import re
from pathlib import Path

ROOT = Path('src')

STUDIO_MODULES: dict[str, str] = {
    'CreationForm': 'form', 'ArgField': 'form', 'TemplatePicker': 'form',
    'StudioTemplatePicker': 'form', 'IntentTrace': 'form', 'useStudioTemplates': 'form',
    'PipelinePanel': 'pipeline', 'PipelineRunPanel': 'pipeline', 'StreamPanel': 'pipeline',
    'GenerationHistory': 'pipeline', 'InspectorPanel': 'pipeline', 'ArtifactDock': 'pipeline',
    'pipelineNodeMeta': 'pipeline',
    'useCreationStudio': 'hooks',
    'types': 'lib', 'constants': 'lib', 'templates': 'lib', 'templateTasks': 'lib',
    'templatePickerMeta': 'lib', 'schemaFields': 'lib', 'executionSpecs': 'lib',
    'artifactTier': 'lib', 'studioFoldHint': 'lib', 'pendingGenerate': 'lib',
    'pendingPipelineGenerate': 'lib', 'restoreRecords': 'lib', 'sessionHistory': 'lib',
    'pipeline': 'lib',
}

AGENT_DETAIL_MODULES: dict[str, str] = {
    'DetailHeader': 'components', 'ConfigPanel': 'components', 'PreviewPanel': 'components',
    'CapabilityCards': 'components', 'ModelSelectSection': 'components', 'ModelSelectCard': 'components',
    'ToolSelectSection': 'components', 'SelectedToolCard': 'components', 'SessionHistoryList': 'components',
    'ParamField': 'components', 'AddToolModal': 'components', 'EditToolDefaultsModal': 'components',
    'SaveOfficialAgentNameModal': 'components', 'ToolDefaultsForm': 'components', 'ToolParamMediaPicker': 'components',
    'useAgentDetailModel': 'hooks', 'useAgentDetailPromptShell': 'hooks',
    'usePreviewChat': 'hooks', 'useSessionListPagination': 'hooks',
    'accentStyles': 'lib', 'configCompare': 'lib', 'modelMeta': 'lib', 'toolMeta': 'lib',
    'toolDefaultsUtils': 'lib', 'pendingOfficialAgent': 'lib',
}

PLAYGROUND_MODULES: dict[str, str] = {
    'ChatArea': 'chat', 'ChatEmptyState': 'chat', 'ChatAttachmentBar': 'chat', 'AskUserCard': 'chat',
    'Sidebar': 'sidebar',
    'RunInspector': 'inspector', 'FreeConfigPanel': 'inspector',
    'VersionDetailModal': 'version', 'VersionManagerDrawer': 'version', 'VersionConfigOverview': 'version',
    'NewAgentModal': 'modals', 'SaveAgentModal': 'modals', 'OverrideModal': 'modals', 'SessionConfigModal': 'modals',
    'WorkflowDetailCard': 'workflow',
}

IMPORT_RE = re.compile(r"(from\s+['\"])(\.(?:\.|/)[^'\"]+)(['\"])")

EXTERNAL_REWRITES = [
    ('OfficialExperience/studio/pipelineNodeMeta', 'OfficialExperience/studio/pipeline/pipelineNodeMeta'),
    ('OfficialExperience/studio/artifactTier', 'OfficialExperience/studio/lib/artifactTier'),
    ('OfficialExperience/studio/templateTasks', 'OfficialExperience/studio/lib/templateTasks'),
    ('OfficialExperience/studio/executionSpecs', 'OfficialExperience/studio/lib/executionSpecs'),
    ('OfficialExperience/studio/schemaFields', 'OfficialExperience/studio/lib/schemaFields'),
    ('OfficialExperience/studio/ArgField', 'OfficialExperience/studio/form/ArgField'),
    ('OfficialExperience/studio/types', 'OfficialExperience/studio/lib/types'),
    ('OfficialExperience/studio/constants', 'OfficialExperience/studio/lib/constants'),
    ('OfficialExperience/studio/templates', 'OfficialExperience/studio/lib/templates'),
    ('OfficialExperience/studio/studioFoldHint', 'OfficialExperience/studio/lib/studioFoldHint'),
    ('OfficialExperience/studio/useStudioTemplates', 'OfficialExperience/studio/form/useStudioTemplates'),
    ('OfficialExperience/studio/StudioTemplatePicker', 'OfficialExperience/studio/form/StudioTemplatePicker'),
    ('OfficialExperience/studio/GenerationHistory', 'OfficialExperience/studio/pipeline/GenerationHistory'),
    ('OfficialExperience/studio/ArtifactDock', 'OfficialExperience/studio/pipeline/ArtifactDock'),
    ('OfficialExperience/studio/pipeline', 'OfficialExperience/studio/lib/pipeline'),
    ('studio/pipelineNodeMeta', 'studio/pipeline/pipelineNodeMeta'),
    ('studio/artifactTier', 'studio/lib/artifactTier'),
    ('studio/templateTasks', 'studio/lib/templateTasks'),
    ('studio/executionSpecs', 'studio/lib/executionSpecs'),
    ('studio/schemaFields', 'studio/lib/schemaFields'),
    ('studio/ArgField', 'studio/form/ArgField'),
    ('studio/types', 'studio/lib/types'),
    ('studio/constants', 'studio/lib/constants'),
    ('studio/templates', 'studio/lib/templates'),
    ('studio/studioFoldHint', 'studio/lib/studioFoldHint'),
    ('studio/useStudioTemplates', 'studio/form/useStudioTemplates'),
    ('studio/StudioTemplatePicker', 'studio/form/StudioTemplatePicker'),
    ('studio/GenerationHistory', 'studio/pipeline/GenerationHistory'),
    ('studio/ArtifactDock', 'studio/pipeline/ArtifactDock'),
    ('AgentDetail/toolMeta', 'AgentDetail/lib/toolMeta'),
    ('AgentDetail/configCompare', 'AgentDetail/lib/configCompare'),
    ('AgentDetail/useSessionListPagination', 'AgentDetail/hooks/useSessionListPagination'),
    ('AgentDetail/pendingOfficialAgent', 'AgentDetail/lib/pendingOfficialAgent'),
    ('AgentDetail/SessionHistoryList', 'AgentDetail/components/SessionHistoryList'),
    ('AgentPlayground/components/ChatArea', 'AgentPlayground/components/chat/ChatArea'),
    ('AgentPlayground/components/ChatEmptyState', 'AgentPlayground/components/chat/ChatEmptyState'),
    ('AgentPlayground/components/ChatAttachmentBar', 'AgentPlayground/components/chat/ChatAttachmentBar'),
    ('AgentPlayground/components/AskUserCard', 'AgentPlayground/components/chat/AskUserCard'),
    ('AgentPlayground/components/Sidebar', 'AgentPlayground/components/sidebar/Sidebar'),
    ('AgentPlayground/components/RunInspector', 'AgentPlayground/components/inspector/RunInspector'),
    ('AgentPlayground/components/FreeConfigPanel', 'AgentPlayground/components/inspector/FreeConfigPanel'),
    ('AgentPlayground/components/VersionDetailModal', 'AgentPlayground/components/version/VersionDetailModal'),
    ('AgentPlayground/components/VersionManagerDrawer', 'AgentPlayground/components/version/VersionManagerDrawer'),
    ('AgentPlayground/components/VersionConfigOverview', 'AgentPlayground/components/version/VersionConfigOverview'),
    ('AgentPlayground/components/NewAgentModal', 'AgentPlayground/components/modals/NewAgentModal'),
    ('AgentPlayground/components/SaveAgentModal', 'AgentPlayground/components/modals/SaveAgentModal'),
    ('AgentPlayground/components/OverrideModal', 'AgentPlayground/components/modals/OverrideModal'),
    ('AgentPlayground/components/SessionConfigModal', 'AgentPlayground/components/modals/SessionConfigModal'),
    ('AgentPlayground/components/WorkflowDetailCard', 'AgentPlayground/components/workflow/WorkflowDetailCard'),
]


def file_subdir(rel: str) -> str:
    parts = Path(rel).parts
    return parts[0] if len(parts) > 1 else ''


def resolve_path(from_dir: str, module: str, module_map: dict[str, str]) -> str | None:
    if module not in module_map:
        return None
    target = module_map[module]
    if from_dir == target:
        return f'./{module}'
    if from_dir == '':
        return f'./{target}/{module}' if target else f'./{module}'
    if target == '':
        return f'../{module}'
    if from_dir == target:
        return f'./{module}'
    return f'../{target}/{module}'


def fix_relative_imports(content: str, file_rel: str, module_map: dict[str, str], root_name: str) -> str:
    from_dir = file_subdir(file_rel)

    def repl(m: re.Match[str]) -> str:
        prefix, path, suffix = m.group(1), m.group(2), m.group(3)
        if path.endswith('CreationStudio.module.less'):
            return f'{prefix}{("../" if from_dir else "./")}CreationStudio.module.less{suffix}' if from_dir else m.group(0)
        if path.endswith('AgentDetail.module.less'):
            depth = 1 if from_dir else 0
            return f'{prefix}{"../" * (depth or 1)}AgentDetail.module.less{suffix}' if from_dir or True else m.group(0)
        if path.endswith('ClothingDupeStudio.module.less') or '/keffects/' in path or '/clothingDupe/' in path:
            return m.group(0)
        if path.endswith('.module.less') and 'ChatEmptyState' in path:
            return m.group(0)

        base = path.split('/')[-1]
        resolved = resolve_path(from_dir, base, module_map)
        if resolved is None:
            return m.group(0)
        return f'{prefix}{resolved}{suffix}'

    return IMPORT_RE.sub(repl, content)


def fix_agent_detail_less(content: str, file_rel: str) -> str:
    if not file_rel.startswith('AgentDetail/'):
        return content
    from_dir = file_subdir(file_rel.replace('AgentDetail/', '', 1))
    if from_dir in {'components', 'hooks', 'lib'}:
        content = content.replace("from './AgentDetail.module.less'", "from '../AgentDetail.module.less'")
    return content


def apply_external(content: str) -> str:
    for old, new in EXTERNAL_REWRITES:
        if old.endswith('/pipeline'):
            content = re.sub(re.escape(old) + r"(?=['\"])", new, content)
        else:
            content = content.replace(old, new)
    content = content.replace('studio/lib/lib/', 'studio/lib/')
    content = content.replace('OfficialExperience/studio/lib/lib/', 'OfficialExperience/studio/lib/')
    return content


def patch_file(path: Path) -> bool:
    text = path.read_text(encoding='utf-8')
    orig = text
    rel = str(path.relative_to(ROOT))

    if '/OfficialExperience/studio/' in rel or rel.startswith('pages/AgentCenter/OfficialExperience/studio/'):
        studio_rel = rel.split('OfficialExperience/studio/', 1)[-1]
        text = fix_relative_imports(text, studio_rel, STUDIO_MODULES, 'studio')

    if '/AgentDetail/' in rel:
        ad_rel = rel.split('AgentDetail/', 1)[-1]
        text = fix_relative_imports(text, ad_rel, AGENT_DETAIL_MODULES, 'AgentDetail')
        text = fix_agent_detail_less(text, rel)

    if '/AgentPlayground/' in rel:
        pg_rel = rel.split('AgentPlayground/', 1)[-1]
        if pg_rel.startswith('components/'):
            pg_rel = pg_rel.replace('components/', '', 1)
            text = fix_relative_imports(text, pg_rel, PLAYGROUND_MODULES, 'playground')

    text = apply_external(text)
    if text != orig:
        path.write_text(text, encoding='utf-8')
        return True
    return False


def main() -> None:
    changed = 0
    for path in ROOT.rglob('*'):
        if path.suffix not in {'.ts', '.tsx'}:
            continue
        if patch_file(path):
            changed += 1
    print(f'Patched {changed} files')


if __name__ == '__main__':
    os.chdir(Path(__file__).resolve().parents[1])
    main()
