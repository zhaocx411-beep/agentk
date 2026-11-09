/**
 * 多模型对战（Mock）
 * 参考 VLM 自动化评测平台：示例卡片 + 匿名双模型回复 + 投票揭名
 */
import { useCallback, useEffect, useMemo, useState } from 'react';
import {
  Alert,
  Button,
  Image,
  Input,
  Modal,
  Tag,
  Typography,
  Upload,
  message,
} from 'antd';
import {
  ArrowUpOutlined,
  PictureOutlined,
  ReloadOutlined,
  ShareAltOutlined,
} from '@ant-design/icons';
import { useSearchParams } from 'react-router-dom';
import PageContainer from '@/components/PageContainer';
import {
  MOCK_ARENA_EXAMPLES,
  MOCK_ARENA_MODELS,
  buildBattleTurnsFromRecord,
  buildMockBattleTurns,
  getBattleRecordById,
  pickRandomExamples,
  pickRandomModelPair,
  resolveModelPair,
} from './mock';
import type { ArenaBattleRecord, ArenaExample, ArenaModel, ArenaPhase } from './types';
import styles from './ModelArena.module.less';

const { Text } = Typography;

function ModelArena() {
  const [searchParams, setSearchParams] = useSearchParams();
  const [activeRecord, setActiveRecord] = useState<ArenaBattleRecord | null>(null);
  const [phase, setPhase] = useState<ArenaPhase>('landing');
  const [examples, setExamples] = useState<ArenaExample[]>(() =>
    pickRandomExamples(MOCK_ARENA_EXAMPLES),
  );
  const [selectedExampleId, setSelectedExampleId] = useState<string | null>(null);
  const [prompt, setPrompt] = useState('');
  const [imagePreview, setImagePreview] = useState<string | null>(null);
  const [modelsOpen, setModelsOpen] = useState(false);
  const [pair, setPair] = useState<[ArenaModel, ArenaModel] | null>(null);
  const [voteChoice, setVoteChoice] = useState<'A' | 'B' | 'tie' | null>(null);

  const applyRecord = useCallback((record: ArenaBattleRecord) => {
    setActiveRecord(record);
    setPrompt(record.prompt);
    setImagePreview(record.imageUrl ?? null);
    setSelectedExampleId(null);

    if (record.status === 'completed') {
      setPair(resolveModelPair(record));
      setPhase('revealed');
      setVoteChoice(record.winner ?? null);
      return;
    }
    if (record.status === 'voting' || record.status === 'running') {
      setPair(resolveModelPair(record));
      setPhase('battling');
      setVoteChoice(null);
      return;
    }
    if (record.status === 'invalid') {
      setPair(null);
      setPhase('landing');
      setVoteChoice(null);
      return;
    }
    setPair(null);
    setPhase('landing');
    setVoteChoice(null);
  }, []);

  useEffect(() => {
    const battleId = searchParams.get('battle');
    if (!battleId) {
      setActiveRecord(null);
      return;
    }
    const record = getBattleRecordById(battleId);
    if (!record) {
      message.warning('未找到该对战记录');
      setSearchParams({}, { replace: true });
      return;
    }
    applyRecord(record);
  }, [applyRecord, searchParams, setSearchParams]);

  const battleTurns = useMemo(() => {
    if (activeRecord && phase !== 'landing') {
      return buildBattleTurnsFromRecord(activeRecord);
    }
    if (!prompt.trim()) return [];
    return buildMockBattleTurns(prompt.trim());
  }, [activeRecord, phase, prompt]);

  const modelALabel = phase === 'revealed' && pair ? pair[0].displayName : '匿名模型 A';
  const modelBLabel = phase === 'revealed' && pair ? pair[1].displayName : '匿名模型 B';

  const resetRound = useCallback(() => {
    setSearchParams({}, { replace: true });
    setActiveRecord(null);
    setPhase('landing');
    setPrompt('');
    setImagePreview(null);
    setSelectedExampleId(null);
    setPair(null);
    setVoteChoice(null);
  }, [setSearchParams]);

  const handleRefreshExamples = () => {
    setExamples(pickRandomExamples(MOCK_ARENA_EXAMPLES));
    setSelectedExampleId(null);
  };

  const handlePickExample = (example: ArenaExample) => {
    setSelectedExampleId(example.id);
    setPrompt(example.prompt);
    setImagePreview(example.imageUrl);
  };

  const handleStartBattle = () => {
    const text = prompt.trim();
    if (!text) {
      message.warning('请输入问题或选择示例');
      return;
    }
    setPair(
      activeRecord?.status === 'draft'
        ? resolveModelPair(activeRecord)
        : pickRandomModelPair(MOCK_ARENA_MODELS),
    );
    setPhase('battling');
    setVoteChoice(null);
    message.success('已开启新一轮匿名对战（Mock）');
  };

  const handleVote = (choice: 'A' | 'B' | 'tie') => {
    setVoteChoice(choice);
    setPhase('revealed');
    message.success('投票已提交，模型名称已揭示（Mock）');
  };

  const handleShare = async () => {
    const mockLink = `${window.location.origin}/model-arena?round=mock_${Date.now()}`;
    try {
      await navigator.clipboard.writeText(mockLink);
      message.success('分享链接已复制（Mock）');
    } catch {
      message.info(mockLink);
    }
  };

  const handleUpload = (file: File) => {
    const url = URL.createObjectURL(file);
    setImagePreview((prev) => {
      if (prev?.startsWith('blob:')) URL.revokeObjectURL(prev);
      return url;
    });
    return false;
  };

  return (
    <PageContainer
      extra={
        <div className={styles.topActions}>
          <Button type="link" onClick={() => setModelsOpen(true)}>
            参战模型({MOCK_ARENA_MODELS.filter((model) => model.enabled).length})
          </Button>
          <Button onClick={resetRound}>新开一轮</Button>
          <Button type="primary" icon={<ShareAltOutlined />} onClick={() => void handleShare()}>
            分享
          </Button>
        </div>
      }
    >
      <div className={styles.page}>
        {activeRecord ? (
          <Alert
            showIcon
            className={styles.revealBanner}
            type={
              activeRecord.status === 'invalid'
                ? 'error'
                : activeRecord.status === 'completed'
                  ? 'success'
                  : 'info'
            }
            message={`当前轮次：${activeRecord.title}`}
            description={
              activeRecord.invalidReason ??
              activeRecord.summary ??
              '从管理列表进入的历史轮次（Mock）'
            }
          />
        ) : null}

        {phase === 'landing' ? (
          <>
            <header className={styles.hero}>
              <Typography.Title level={2} className={styles.heroTitle}>
                多模态模型对战
              </Typography.Title>
              <p className={styles.heroDesc}>
                上传图片并向两个匿名模型提问 → 对话结束后进行投票 → 投票后将展示模型名称
              </p>
              <p className={styles.heroWarn}>
                注意：请勿使用「你是谁？」等可能暴露模型身份的问题。模型身份暴露后该轮投票结果将失效
              </p>
            </header>

            <div className={styles.exampleGrid}>
              {examples.map((example) => (
                <button
                  key={example.id}
                  type="button"
                  className={`${styles.exampleCard} ${
                    selectedExampleId === example.id ? styles.exampleCardActive : ''
                  }`}
                  onClick={() => handlePickExample(example)}
                >
                  <div className={styles.exampleImageWrap}>
                    <img
                      src={example.imageUrl}
                      alt=""
                      className={styles.exampleImage}
                      loading="lazy"
                    />
                  </div>
                  <div className={styles.examplePrompt}>{example.prompt}</div>
                </button>
              ))}
            </div>

            <div className={styles.refreshRow}>
              <Button icon={<ReloadOutlined />} onClick={handleRefreshExamples}>
                换一换
              </Button>
            </div>
          </>
        ) : (
          <div className={styles.stage}>
            {phase === 'revealed' ? (
              <Alert
                type="success"
                showIcon
                className={styles.revealBanner}
                message="投票完成"
                description={
                  voteChoice === 'tie'
                    ? '你选择了平局。'
                    : `你选择了 ${voteChoice === 'A' ? modelALabel : modelBLabel}。`
                }
              />
            ) : null}

            <div className={styles.userPromptBar}>
              <div className={styles.userPromptLabel}>本轮问题</div>
              <p className={styles.userPromptText}>{prompt}</p>
              {imagePreview ? (
                <Image
                  src={imagePreview}
                  alt="本轮图片"
                  className={styles.previewThumb}
                  preview={{ mask: false }}
                />
              ) : null}
            </div>

            <div className={styles.battleBoard}>
              <section className={styles.modelColumn}>
                <div className={styles.modelColumnHead}>
                  <span className={styles.modelColumnTitle}>{modelALabel}</span>
                  {phase === 'revealed' && pair ? <Tag color="purple">{pair[0].provider}</Tag> : null}
                </div>
                <div className={styles.modelColumnBody}>
                  {battleTurns.find((turn) => turn.role === 'modelA')?.text}
                </div>
              </section>
              <section className={styles.modelColumn}>
                <div className={styles.modelColumnHead}>
                  <span className={styles.modelColumnTitle}>{modelBLabel}</span>
                  {phase === 'revealed' && pair ? <Tag color="blue">{pair[1].provider}</Tag> : null}
                </div>
                <div className={styles.modelColumnBody}>
                  {battleTurns.find((turn) => turn.role === 'modelB')?.text}
                </div>
              </section>
            </div>

            {phase === 'battling' ? (
              <div className={styles.voteBar}>
                <Text type="secondary">请选择更优回答：</Text>
                <Button onClick={() => handleVote('A')}>模型 A 更好</Button>
                <Button onClick={() => handleVote('B')}>模型 B 更好</Button>
                <Button onClick={() => handleVote('tie')}>平局</Button>
              </div>
            ) : (
              <div className={styles.voteBar}>
                <Button type="primary" onClick={resetRound}>
                  再来一轮
                </Button>
              </div>
            )}
          </div>
        )}

        {phase === 'landing' ? (
          <div className={styles.composerDock}>
            {imagePreview ? (
              <div className={styles.composerMeta}>
                <img src={imagePreview} alt="" className={styles.previewThumb} />
                <Button
                  type="link"
                  size="small"
                  onClick={() => {
                    if (imagePreview.startsWith('blob:')) URL.revokeObjectURL(imagePreview);
                    setImagePreview(null);
                  }}
                >
                  移除图片
                </Button>
              </div>
            ) : null}
            <div className={styles.composerShell}>
              <Upload accept="image/*" showUploadList={false} beforeUpload={handleUpload}>
                <Button
                  type="text"
                  className={styles.attachBtn}
                  icon={<PictureOutlined />}
                  aria-label="上传图片"
                />
              </Upload>
              <Input.TextArea
                className={styles.composerInput}
                value={prompt}
                onChange={(event) => setPrompt(event.target.value)}
                placeholder="使用左侧按钮上传图片，Enter 发送，Shift+Enter 换行"
                autoSize={{ minRows: 1, maxRows: 4 }}
                onPressEnter={(event) => {
                  if (event.shiftKey) return;
                  event.preventDefault();
                  handleStartBattle();
                }}
              />
              <Button
                type="primary"
                className={styles.sendBtn}
                icon={<ArrowUpOutlined />}
                aria-label="发送"
                onClick={handleStartBattle}
              />
            </div>
          </div>
        ) : null}
      </div>

      <Modal
        title="参战模型"
        open={modelsOpen}
        onCancel={() => setModelsOpen(false)}
        footer={null}
        width={560}
      >
        <div className={styles.modelList}>
          {MOCK_ARENA_MODELS.map((model) => (
            <div key={model.id} className={styles.modelListItem}>
              <div>
                <div className={styles.modelListName}>{model.displayName}</div>
                <div className={styles.modelListMeta}>
                  {model.provider} · {model.modality.toUpperCase()}
                </div>
              </div>
              <Tag color={model.enabled ? 'green' : 'default'}>
                {model.enabled ? '可参战' : '已停用'}
              </Tag>
            </div>
          ))}
        </div>
      </Modal>
    </PageContainer>
  );
}

export default ModelArena;
