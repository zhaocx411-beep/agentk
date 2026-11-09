// 各阶段 prompt 模板，逐段移植自 agentq drama-workflow（qai-sdk）：
// costume.ts / split-script.ts / simplify.ts / asset-desc.ts / storyboard.ts /
// first-frames.ts / video-clips.ts / asset-images.ts。正文与那边保持逐字一致，
// 只有输出格式尾部按 generate_text 的 outputSchema 约束改为 object 包装。
// 改动前先对照 qai-sdk 原文想清楚为什么。
package worker

import (
	"fmt"
	"strings"
)

// defaultStyleAnchor 用户未指定风格时的全剧视觉锚点
// （qai style.ts 默认预设 japanese-anime-cel「日漫赛璐珞风」原文）。
const defaultStyleAnchor = "高品质日漫赛璐珞风格，清晰线稿、平涂色块与利落阴影，" +
	"角色五官精致，表情张力强，背景细节丰富，动画电影级构图与光影，避免写实CG质感。"

// threeViewSpec 定妆照统一构图：正/侧/背三视图（qai costume.ts THREE_VIEW_SPEC 原文）。
const threeViewSpec = "本图为角色三视图设定图：同一人物在同一张横版图中从左到右并排呈现正面、正侧面、背面三个视角，" +
	"三个视角必须是同一个人、同一套服装、同一发型与配饰，站在同一水平线上、身高一致，" +
	"全身完整入镜含鞋子，彼此留出清晰间距互不遮挡。" +
	"正面视角面部完全朝前、五官正对镜头；侧面视角为正侧面90度；背面视角完全背对镜头，展示发型背面与服装背部结构。" +
	"三个视角均为自然站立姿态、双手自然垂放，构图均匀居中，" +
	"画面中不得出现任何文字、标注、分割线，除这三个视角外不得出现其他人物，纯白色背景无投影。"

// threeViewRefNote 定妆三视图作为参考图使用时必须附带的说明
// （qai costume.ts THREE_VIEW_REF_NOTE 原文）。
const threeViewRefNote = "【关于人物定妆参考图】标注为「定妆三视图」的参考图是该角色的三视图设定图，" +
	"同一个人的正面、侧面、背面并排，这三个身位是同一个人的多角度展示，不是三个不同的人。" +
	"生成结果里该角色只能出现一次，不得复刻三视图的并排排版，不得出现同一人物的多个分身。\n"

// consistencyPrefix 人物状态图 edit_image 的一致性强调前缀
// （qai asset-images.ts CONSISTENCY_PREFIX 原文）。
const consistencyPrefix = "【严格保持人物一致性——最高优先级指令】\n" +
	"本次生成以下方参考图为唯一外观标准，任何情况下不得修改：\n" +
	"· 人物五官（眼型、眉形、鼻梁、唇型、脸型轮廓）必须与参考图完全一致\n" +
	"· 发型发色发冠（束法、高度、饰品形状颜色）必须与参考图完全一致\n" +
	"· 服装配色与款式（颜色、领型、纹样、层次）必须与参考图完全一致\n" +
	"· 仅允许调整：姿态、表情、镜头角度、所处场景背景\n" +
	"· 拒绝任何自由发挥的外貌改动，即便提示词有隐含空间\n" +
	"· 本次输出必须是单人、单视角的完整画面\n\n" +
	threeViewRefNote +
	"\n【以下为本次资产图生成内容描述】\n"

// styleAnchorOrDefault 归一用户风格输入：空则用默认日漫赛璐珞锚点。
func styleAnchorOrDefault(stylePrompt string) string {
	if s := strings.TrimSpace(stylePrompt); s != "" {
		return s
	}
	return defaultStyleAnchor
}

// withVisualStyle 给生图 prompt 追加风格锚点（qai style.ts withVisualStyle 语义）：
// prompt 已含锚点时不重复追加。
func withVisualStyle(prompt, styleAnchor string) string {
	cleanPrompt := strings.TrimSpace(prompt)
	cleanStyle := strings.TrimSpace(styleAnchor)
	if cleanStyle == "" || strings.Contains(cleanPrompt, cleanStyle) {
		return cleanPrompt
	}
	return cleanPrompt + "\n\n【全剧统一视觉风格】\n" + cleanStyle +
		"\n所有人物、场景、道具、首帧和视频镜头必须保持这一风格。"
}

/* ---------- 阶段 1：完整多集剧本（qai drama-script SKILL 的输出规范） ---------- */

// buildScriptPrompt 把题材 / 梗概扩写成分集完整剧本；用户已给完整剧本时
// 只做格式整理。分集格式硬性要求移植自 qai drama-script SKILL.md。
func buildScriptPrompt(brief, title string, episodes int) string {
	titleLine := ""
	if strings.TrimSpace(title) != "" {
		titleLine = "作品名固定为《" + strings.TrimSpace(title) + "》。\n"
	}
	return fmt.Sprintf(`请根据以下用户输入创作一部 %d 集漫剧的完整可拍摄剧本。

【用户输入】
%s

%s创作流程（内部推理，产出只保留最终剧本）：
1. 人物定位分析：主角 1-2 人 + 关键配角/反派 2-3 人，总人数控制在 5 人以内（每个人物都会生成定妆照，控制成本）。每个人物：姓名（全剧唯一且稳定）、身份、外貌与服饰特征、性格与动机、人物关系。命名避开真实名人与知名 IP。
2. 全剧整体大纲：核心冲突、主角目标、爽点类型（打脸/逆袭/复仇/甜宠等）、结局走向。节奏遵循短剧法则：开场 30 秒内抛出钩子，冲突层层升级，高潮放在最后两集之前，最终集收束。
3. 分集大纲：每集本集核心事件、冲突升级点、结尾钩子（悬念/反转，驱动观众看下一集）。
4. 逐集剧本：每写完一集，先更新「一致性状态卡」（各人物当前状态/位置/知情程度、未回收的伏笔），再写下一集，保证前后连贯。

单集规范：
- 800-2000 字，绝不超过 2000 字；3-5 场戏；节奏快，无注水
- 台词口语化短句，情绪外显；关键信息用台词或旁白交代，不靠画面外脑补
- 场景数量克制（每集 2-3 个场景，跨集尽量复用同名场景——每个场景都会生成场景图资产）

输出格式（硬性要求，直接对接漫剧流水线）：
- 每集以「第X集」单独成行开头（流水线按此切集，缺失会导致切割失败）
- 场景行：集号-场号 + 场景名 + 日/夜 + 内/外；紧跟「人物：」行列出本场人物
- 动作与画面描述用【】包住；台词写成 角色名："台词"（引号必须有）；旁白用 旁白：
- 人物名与场景名全剧统一，不要中途改名

用户输入已是完整剧本（含「第X集」分集标记）时，保留其核心情节、人物关系和关键台词，只做格式整理和必要补全，不得擅自增删集数。

输出 JSON：title 是作品名（用户指定时照抄），synopsis 是一句话全剧梗概，script 是完整分集剧本正文（每集以「第X集」单独成行开头）。`,
		episodes, brief, titleLine)
}

/* ---------- 阶段 2：人物定妆描述 + 固定音色（qai costume.ts 原文移植） ---------- */

func buildCostumePrompt(scriptContent, visualStyle string) string {
	return scriptContent + "\n" +
		`现在我需要制作某部漫剧的人物定妆照。
上面是漫剧的剧本，请你提取出剧本中出现的所有非群像性非纯过场NPC的人物，然后提取出这些人物的小传和人物特征，进而给出人物的生图描述。

【全剧统一视觉风格——最高优先级】：
` + visualStyle + `
每一条人物 prompt 都必须完整体现上述视觉风格，不得沿用示例里的古风题材或其它画风；服装、时代与场景仍以用户剧本为准。

【人物提取标准——必须包含以下所有类型，不得遗漏】：
（1）主角、男女主角：必须提取
（2）有名字且在多个场景出现的配角：必须提取（例如：贴身侍从、反派家眷、管事等有具体姓名的人物）
（3）有名字且有实质性台词或行动的人物：必须提取（例如：刀疤脸管事、心腹嬷嬷、府医、丫鬟小翠等有名字或绰号的人物）
（4）无名但有独特外貌描述、在多个场景反复出现的人物：必须提取
（5）系统、AI等无实体形象的角色：无需提取，跳过
（6）只出现一次且只有一两句台词、没有名字或绰号的纯路人（护卫A、家丁、侍女等只有"是！"之类的集体回应）：可跳过

生图描述的示例：
1. 沈倦
青年男性，将门之子沈倦，身姿挺拔硬朗，面容俊朗刚毅，眉眼冷冽，眼底藏着失望与隐忍，眸光锐利澄澈。乌黑长发高束，搭配简约黑色皮质束发冠，造型干净利落，无多余繁复配饰。身着藏青色暗纹古风武将常服，衣料厚实垂坠，肩头制式标识脱落，衣衫轻微凌乱，腰间配剑空置，袖口收紧贴合手腕。面容干净清爽，剑眉星目，唇线紧抿，神色肃穆坚定。脚踩黑色厚底云纹古风皂靴。` + visualStyle + threeViewSpec + `
2. 慕锦言
青年女性，沈倦之妻慕锦言，身形纤细却脊背挺直，气质沉静如水，眼神坚定清澈，流露出超乎年龄的智慧与从容。乌黑长发盘成简约利落的发髻，仅以一根素雅木簪固定，几缕碎发垂于鬓边。身着素色棉麻布裙，款式朴素但洁净，外罩一件浅灰色褙子以御风寒，袖口微微挽起。面容清丽，淡雅素颜，眉形温婉，唇色自然，神情平静中带着一丝忧虑，但更多的是决绝。脚踩一双软底布鞋。` + visualStyle + threeViewSpec + `

注意，你生成的图片是人物整部剧整体的定妆照，与人物所处的情节无关。除非有（1）人物年龄发生巨大变化（2）人物遭遇巨大变故且变故前后都占有剧本的较大篇幅（3）人物容貌/造型在情节中发生明确的变化。这几种情况下，你可以单个人物有多个生图描述产生多张定妆照。除此之外，你每个人物只有一张定妆照。

【定妆照状态要求——极其重要】：
每一张定妆照描述的都是该人物的"日常正常状态"，即该人物平时的常规装扮与神态。
严禁以下任何特殊状态作为定妆照：
- 暴怒/愤怒到失控的状态
- 囚禁/被铁链锁住/穿囚服的状态
- 濒死/重伤/血迹斑斑/气息奄奄的状态
- 发狂/蛊毒发作/神志不清的状态
- 昏迷/晕倒的状态
- 任何极端情绪爆发或身体极度受损的状态
定妆照应展现人物在正常剧情中的日常形象：穿着日常服装，神情自然（可以符合人物性格，如冷漠/傲慢/温和等），身体完整健康，无任何伤痕或束缚。

每张图片描述都必须包含完整的全剧统一视觉风格，并以以下三视图规范结尾：
` + threeViewSpec + `

重要提示：你提取的人物切勿有遗漏！凡是剧本场景人物列表中明确列出姓名或绰号的人物，一律提取。

除定妆照外，你还要为每个有台词的人物设定一个全剧固定的嗓音。
要求：
（1）name 与 costumes 中的名字完全一致，不要加括号状态说明；同一人物无论有几张定妆照，只写一条
（2）必须额外写一条 name 为"旁白"的条目，作为旁白配音
（3）age 填年龄段，如 少年/青年/中年/老年
（4）timbre 20~40字，需覆盖：音高（低沉/中音/清亮）、音色质感（沙哑/清透/厚实/带颗粒感）、语速、气息与咬字特点
（5）timbre 严禁写情绪、心情或场景（如"愤怒地""悲伤地"）——情绪由每个分镜单独决定，嗓音本身全剧不变
（6）不同人物的嗓音要有明显区分度，同性别同年龄段的人物必须在音高或质感上拉开差距
（7）嗓音要贴合人物的性别、年龄、身份与性格

只输出严格 JSON 对象，不要输出 markdown、解释、思考过程或其他文字：
{
  "costumes": [
    {
      "name": "人物姓名（不含状态说明）",
      "variant": "仅在确有长期造型变化时填写，否则为空字符串",
      "prompt": "包含完整外貌、服装、风格和三视图规范的生图描述"
    }
  ],
  "voices": [
    {
      "name": "人物名或旁白",
      "gender": "男/女/空字符串",
      "age": "少年/青年/中年/老年/空字符串",
      "timbre": "20~40字固定嗓音描述"
    }
  ]
}`
}

// composePortraitPrompt 拼出定妆照最终生图 prompt：外貌描述（已由 costume
// 阶段带出风格与三视图规范）+ withVisualStyle 保险追加。
func composePortraitPrompt(portraitPrompt, styleAnchor string) string {
	prompt := withVisualStyle(portraitPrompt, styleAnchor)
	if !strings.Contains(prompt, "三视图") {
		prompt += "\n" + threeViewSpec
	}
	return prompt
}

/* ---------- 阶段 3：切集（qai split-script.ts 兜底 prompt 原文） ---------- */

// buildSplitPrompt LLM 兜底切集（确定性正则切分失败时才用）。
func buildSplitPrompt(scriptContent string) string {
	return `以下是一部漫剧的完整多集剧本，请将其按集数拆分。

要求：
1. 识别每一集的起始和结束位置（通常以"第X集"、"EP X"、"第X话"等标记区分）
2. 将每一集的完整内容（包含集名标题）提取出来
3. 以严格的 JSON 格式输出，不要输出任何其他内容

输出 JSON：episodes 数组，每项含 ep（集号数字）、title（集名）、content（该集完整剧本内容，原封不动照抄，不要删减任何内容）。

以下是完整剧本：

` + scriptContent
}

/* ---------- 分集阶段 a：图片资产描述（qai asset-desc.ts 原文移植） ---------- */

func buildAssetDescPrompt(
	prev2ScriptBlock, prev2AssetBlock, prev1ScriptBlock, prev1AssetBlock, currentScript string,
	visualStyle string,
) string {
	return `现在我需要制作某部漫剧某一集的人物/场景/图片资产图，我需要你输出这些资产图的图片描述。
【全剧统一视觉风格——最高优先级】
` + visualStyle + `
所有新生成资产的 prompt 必须体现这一风格。下方古风参考仅用于理解 JSON 结构、资产拆分和复用逻辑，不得复制其时代、服装或画风。

我给你一集漫剧作为参考：
【参考漫剧剧本与资产图描述】
【剧本】
第1集：麒麟坠落

1-1 大将军府-议事堂 日 内

人物：
沈倦：被构陷的将门之子
沈渊：大将军，沈倦之父
沈瑾：沈倦二弟，伪善
沈炬：沈倦三弟，帮凶
慕锦言：沈倦之妻

【开场，镜头快速扫过堂内肃杀气氛，最终定格在跪于堂中的沈倦身上，他肩上的衔章和佩剑被丢在一旁。】

旁白： 我，沈倦，曾被誉为将门麒麟。如今，却因兄弟构陷，跪在这冰冷的议事堂，等待父亲的审判。

【沈渊将书信账册狠狠摔在沈倦面前。】

沈渊： （雷霆之怒）孽子！你还有何话可说！私通敌寇，泄露军机，贪墨粮饷！我沈家世代忠良，怎么就出了你这么个无君无父、无情无义的畜生！

沈瑾： （上前一步，假意劝说）父亲息怒，大哥或许只是一时糊涂，您别气坏了身子。

沈炬： （立刻附和）是啊父亲，大哥一向受您器重，怎会做出此等事来？这里面……会不会有什么误会？

【沈瑾与沈炬对视一眼，嘴角闪过一丝不易察觉的得意。】

沈倦： （缓缓抬头，声音冷静）父亲，敢问二弟、三弟呈上的证物，您可曾仔细看过？

沈渊： 证据确凿，还容你狡辩？！

沈倦： 儿臣不敢狡辩，只是想请父亲看个明白。那封通敌信函，模仿儿臣笔迹，确有七分相似。但儿臣惯用狼毫，笔锋锐利，收笔处常有回锋。而此信笔迹圆滑，力道虚浮，分明是羊毫所书。军中之人，谁不知我沈倦的'狼毫铁画'？

沈瑾： （脸色微变）大哥常年征战，换一种笔有什么稀奇？或许是情急之下……

沈倦： （目光如刀，打断）那么敢问二弟，信上所盖的私印，为何是我三年前便已停用的旧印？此印当年因一角破损，早已封存入库，三年来从未动用，军机要务皆用新印。二弟、三弟常年留守京中，或许不知，但父亲掌管帅印，应是一清二楚。

【沈渊闻言神色一滞。】

沈倦： 再说账册。上面记录我贪墨三万两白银，但支出日期，是上月初三。而那一日，我正率麒麟营在榆关外与敌军主力鏖战，此事军中人人可以作证。儿臣分身乏术，如何能在千里之外，亲自督办粮饷之事？三弟掌管府中账目，敢问三弟，我的手令与批文何在？

沈炬： （眼神闪躲）大哥……你位高权重，绕过一两道程序，也并非难事……

沈倦： （冷笑）好一个并非难事！我沈倦治军，军规如铁，何时绕过程序？父亲，这些漏洞百出的伪证，您当真看不出来吗？还是说，在您心里，早已认定了儿臣就是那不忠不孝的逆子？

沈渊： （被戳中心事，猛拍桌案）住口！巧言令色，不知悔改！无论此事真假，你治下不严，御下不察，已是死罪！来人！

沈倦： （眼中闪过失望）父亲！

沈渊： （双目赤红）不必再叫我父亲！我沈渊没有你这样的儿子！传我将令：沈倦，剥夺其麒麟营统领之职，革去一切军衔，从族谱中除名！流放北境苦寒之地，永世不得踏入京城半步！

【两名亲兵上前，粗暴地架起沈倦。】

慕锦言（画外音）： 等等。

【众人惊愕，只见慕锦言抱着尚在襁褓的婴孩，从侧廊缓缓走出。】

慕锦言： （无视众人，对沈渊福身）父亲，媳妇有一言相告。

沈渊： （怒气未消）这里没有你说话的份，退下！

慕锦言： （直起身，目光坚定）嫁入沈家时，我曾对倦哥说，夫妻一体，荣辱与共。如今他要去北境，我与孩儿，自当随行。

沈渊： （怒极反笑）流放之路，九死一生！北境苦寒，寸草不生！你一个娇生惯养的女子，还带着个奶娃娃，是去送死吗？！

慕锦言： （语气平静而决绝）生，我们一家人一起生。死，我们一家人也一起死。大将军府的富贵荣华，我们夫妻担不起，这便还给您。

【慕锦言不再看沈渊，毅然走向沈倦，为他理了理有些凌乱的衣领。】

慕锦言： （柔声）夫君，我们走吧。

【镜头特写：沈倦看着妻子坚定的眼神，再看看怀中的儿子，眼中由死寂变为燃起一丝火焰。一家三口在众人目光下，决绝地走向门外刺眼的阳光。】

【本集资产图描述】
剧集	类别	角色/场景/道具	最终定稿AI绘图提示词（可直接出图）
第1集 麒麟坠落	人物定妆	沈倦（议事堂受审）	古风正剧动漫平涂风风，8K高清，电影级光影质感，全身人物定妆照，细节精致，构图高级。青年男性沈倦，身姿挺拔硬朗，面容俊朗刚毅，眉眼冷冽，眼底藏着失望与隐忍，眸光锐利澄澈。乌黑长发高束，搭配简约黑色皮质束发冠，造型干净利落，无多余繁复配饰。身着藏青色暗纹古风武将常服，衣料厚实垂坠，肩头制式标识脱落，衣衫轻微凌乱，腰间配剑空置，袖口收紧贴合手腕。面容干净清爽，英气淡颜古风妆，剑眉星目，唇线紧抿，神色肃穆坚定。脚踩黑色厚底云纹古风皂靴，单膝跪立于清冷青石地面，周身气质清冷孤傲、沉稳倔强。背景为庄严肃穆的古代将军府议事堂，整体氛围沉郁肃杀。
第1集 麒麟坠落	人物定妆	沈倦（离场决绝）	古风正剧动漫平涂风风，8K高清，户外自然光侧光，全身定妆照。青年男性沈倦，五官不变，眉宇褪去隐忍，眼底燃起微光，神情坚定倔强。高束黑发配玄色简约发带，藏青色旧款武将常服褶皱自然，略带风尘感。英气淡妆，身姿挺直沉稳，黑色云纹皂靴，侧头温柔看向身旁妻子，背景为将军府大门，强光逆光氛围，苍凉倔强，细节精致。
第1集 麒麟坠落	人物定妆	沈渊（大将军）	古风正剧动漫平涂风风，8K电影质感，全身定妆照。中老年男性，面容威严沧桑，线条凌厉，双目赤红暗藏复杂心绪，震怒且隐忍。黑发挽起，墨玉镶铜纹武将高冠，鬓角微白。身着玄色织金蟒纹大将军朝服，厚重华贵，气场压迫感极强。威严古风妆容，眉峰下压，面色沉郁，端坐实木案几后，手掌按桌，满含怒火与深沉算计，背景将军府议事堂。
第1集 麒麟坠落	人物定妆	沈瑾（伪善二哥）	古风正剧动漫平涂风风，8K高清，室内柔光，半身定妆照。青年男子，面容俊秀斯文，表面恭顺温和，眼底藏阴鸷得意，皮笑肉不笑。黑发规整高束，素雅玉质发簪，儒雅规整。身着月白色暗纹锦缎公子长衫，面料精致华贵。清俊书生淡妆，眉眼柔和，躬身假意劝和，站姿文雅，心机暗藏，与其他兄弟五官差异明显。
第1集 麒麟坠落	人物定妆	沈炬（怯懦三弟）	古风正剧动漫平涂风风，8K高清，室内冷光，半身定妆照。青年世家公子，脸型圆润柔和，下颌无锋利棱角，钝感五官，与沈倦英气五官反差极大。圆杏下垂眼，眼神闪躲心虚，局促不安，短淡平眉，鼻梁圆钝，唇薄微抿。黑发简约束发，黑色哑光锦带固定，无华丽配饰。身着浅灰暗纹柔锦公子长袍，低调显贵不张扬，站姿微缩落后，怯懦畏缩，心底有鬼的神态。
第1集 麒麟坠落	人物定妆	慕锦言（进言决绝）	古风正剧动漫平涂风风，8K电影光影，全身定妆照。古典清丽美人，眉眼温婉大气，眼神果敢坚定，毫无惧色。标准古风妇人发髻，素银小花发簪点缀，素雅干净。身着水青色交领襦裙，外搭浅纱披帛，端庄素雅。清透古风淡妆，柳叶眉浅唇色，气场沉稳。怀抱襁褓婴儿，身姿挺立厅堂中央，神情决绝从容。
第1集 麒麟坠落	场景	将军府议事堂	古风古代大将军府议事堂全景，动漫平涂风影视布景，实木雕花大案几，案上摆放书信、账册卷宗，青石地砖，肃穆立柱，暗沉帷幔，光线昏暗，氛围压抑肃杀，8K电影构图，细节饱满。
第1集 麒麟坠落	道具	伪证信函+账册	古风文房特写，泛黄古宣纸书信，羊毫书写字迹，老旧线装账册，摆放实木案台，光影明暗交错，古旧真实质感，影视道具特写，细节清晰。
第1集 麒麟坠落	道具	襁褓婴儿（幼年沈明庭）	古风动漫平涂风特写，素色云纹棉布襁褓，包裹熟睡幼婴，布料柔软古朴，暖柔光氛围，神态安稳，细节真实。
第1集 麒麟坠落	人物	府兵亲兵	古风动漫平涂风群演定妆，青年男性，束发配黑色布质头巾，深灰色粗布亲兵短打，妆容粗犷，神情木然严肃，黑色布靴，架人站姿，肃穆规整。

【严格规则——极其重要，不得违反】
1. 人物类资产图（人物定妆、人物）：图中出现的人物必须是单人，严禁出现多人互动场景。
   - 禁止描述中出现「怀抱他人」「看向某人」「与某人对视」「搀扶」「护着」「推开」等涉及两人或多人关系的动作
   - 禁止生成「母子」「夫妻」「兄弟」等多人入镜的画面
   - 每张人物资产图只描述一个人物站立/坐立于场景中的状态，互动动作改为个人动作（如「怀抱婴儿」改为「双手垂于身前」）
   - 如果剧本中该人物只在多人场景中出现，依然只生成该人物的单人图
2. 场景、道具类资产图：不限，可以有道具组合，但不能出现任何人物身体部位

现在我给你我要写的剧本，请你给我对应的本集资产图描述。我还会给你前面两集的剧本和对应的资产图描述，请你一起参考，并确定能否复用前两集的资产。
` + prev2ScriptBlock + `
` + prev2AssetBlock + `
` + prev1ScriptBlock + `
` + prev1AssetBlock + `
` + currentScript + `

输出 JSON：assets 数组，每项含：
- category："人物状态/场景/道具"
- name：资产名称
- character_ref：人物状态图对应的准确角色名；其他类别填 null
- prompt：完整图片生成提示词；人物状态图必须是融合稳定身份、服装和当前状态的单人物画面
- reuse_from_ep：可复用前集资产时填集号数字，否则 null
- reuse_asset_name：复用时填前集资产的准确名称，否则 null`
}

/* ---------- 分集阶段 b：超长对白精简（qai simplify.ts 原文） ---------- */

func buildSimplifyPrompt(scriptContent string) string {
	return "下面这集剧本可能会有几句超长对白，如果有，请你缩短超长对白的长度，" +
		"也能表达剧本的意图，符合情节发展。你只要缩短单条对话长度即可，" +
		"不需要合并多条对话。如果没有，你无需改动剧本。" +
		"输出 JSON：script 是处理后的完整剧本正文。" +
		scriptContent
}

/* ---------- 分集阶段 c：分镜（qai storyboard.ts 原文移植） ---------- */

func buildStoryboardPrompt(
	currentScript, prev1ScriptBlock, prev1StoryboardBlock string,
	visualStyle string, maxShots int,
) string {
	return fmt.Sprintf(`请把以下单集漫剧剧本改写为可直接生成视频的分镜 JSON。

【全剧统一视觉风格】
%s
first_frame 和 storyboard 的画面描述必须持续体现这一风格，并与人物、场景资产保持一致。

【本集剧本】
%s

【连续性参考】
%s
%s

硬性规则：
1. 整集最多 %d 个视频片段；根据剧情节奏决定实际数量，不要为了凑数拆镜。
2. 每个片段 duration_s 必须是 5~10 秒的整数，由片段动作与台词量决定。
3. 每个片段画面中实际出现并发生交互的具体人物最多 3 人；群演用群体称谓描述，不列入 characters，也不为其加载单独参考图。
4. characters 必须准确列出本片段画面中实际出现、需要人物参考图的角色；仅在旁白中被提及的人不得列入。
5. scenes、props、first_frame 必须由你直接结构化给出。first_frame 只描述片段开始瞬间的静止画面（80~150 字：景别与机位、画面里有谁、各自位置朝向、姿态表情、场景与关键道具、光线氛围），不写后续动作、运镜、台词或字幕；人物一律用本名指代。
6. storyboard 中每个镜头依次使用【景别：XX】【运镜：XX】【人物位置：XX】标注；写清人物初始动作、位置、相对站位和朝向。
7. 台词必须能在 duration_s 内自然说完，禁止异常加速语速。5~6 秒不超过 18 字，7~8 秒不超过 26 字，9~10 秒不超过 34 字。
8. 中文台词，无背景音乐，无字幕；人物、场景名称必须与剧本一致。

输出 JSON：shots 数组，每项含：
- shot_id：两位数字字符串，如 "01"
- duration_s：5~10 的整数
- characters：实际出现在画面中的角色名数组
- scenes：主要场景名数组
- props：影响画面的关键道具名数组
- first_frame：起始静帧描述
- storyboard：完整分镜、动作和台词
- speakers：按首次开口顺序列出的角色名数组；旁白写"旁白"；无台词填空数组`,
		visualStyle, currentScript, prev1ScriptBlock, prev1StoryboardBlock, maxShots)
}

/* ---------- 分集阶段 e：首帧图（qai first-frames.ts 原文移植） ---------- */

// composeFramePrompt 首帧图 prompt：参考图逐图对应 + 静止画面语义 + 风格锚点。
// referenceLabels 与实际提交的参考图顺序一一对应；没有参考图时退化为纯文生图。
func composeFramePrompt(firstFrameDesc string, referenceLabels []string, styleAnchor, aspectRatio string) string {
	var b strings.Builder
	b.WriteString("请画出一部漫剧某个镜头的起始画面，也就是这段视频的第一帧。\n\n")
	if len(referenceLabels) > 0 {
		b.WriteString("【参考图逐图对应关系】\n")
		for i, label := range referenceLabels {
			fmt.Fprintf(&b, "- 图%d：%s\n", i+1, label)
		}
		b.WriteString("人物五官、发型、服装配色必须与对应参考图完全一致，" +
			"场景布局与关键道具外观也要与对应参考图一致，画风与参考图保持统一。\n")
		for _, label := range referenceLabels {
			if strings.Contains(label, "定妆三视图") {
				b.WriteString(threeViewRefNote)
				break
			}
		}
		b.WriteString("\n")
	}
	b.WriteString("【画面内容】\n")
	b.WriteString(firstFrameDesc)
	b.WriteString("\n\n【硬性要求】\n")
	b.WriteString("- 这是一张定格的静止画面，只画镜头开始那一瞬间的状态，不要表现动作过程\n")
	fmt.Fprintf(&b, "- 竖版 %s 全画幅构图，主体居中，上下留出运镜空间，画面铺满画布，无边框留白\n", aspectRatio)
	b.WriteString("- 单一完整画面，不要分格、不要多格漫画、不要拼图、不要画中画\n")
	b.WriteString("- 画面中不得出现任何文字、字幕、对话框、水印、logo\n")
	fmt.Fprintf(&b, "- 全剧统一视觉风格：%s\n", styleAnchor)
	return b.String()
}

/* ---------- 分集阶段 f：视频（qai video-clips.ts buildKlingPrompt 移植） ---------- */

// videoPromptPrefix 首帧锁定段（qai KLING_PREFIX）：首帧给定画面基准，
// 参考图段（如有）再逐图锁定人物、场景、道具身份。
const videoPromptPrefix = "首帧图给定了本镜头的人物、场景、道具与画风基准。" +
	"请严格保持人物与首帧完全一致，包括五官、发型、服装配色等，不得混淆不同角色，尤其不能改变画风；" +
	"场景布局及关键道具外观也应与首帧保持一致。" +
	"运动过程中人物和场景不得漂移成真人、照片或另一种画风。\n"

// buildVideoRefSection 视频参考图逐图对应段：labels 与提交给可灵 Omni 的
// referImages 顺序一一对应，prompt 内按 @image_n 指代（可灵 Omni 的编号
// 规则）。没有参考图时返回空串，退化为纯首帧锁定。
func buildVideoRefSection(referenceLabels []string) string {
	if len(referenceLabels) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("【参考图逐图对应关系】\n")
	for i, label := range referenceLabels {
		fmt.Fprintf(&b, "- @image_%d：%s\n", i+1, label)
	}
	b.WriteString("以上参考图用于锁定对应人物、场景和道具的身份与外观：" +
		"人物五官、发型、服装配色必须与对应参考图完全一致，" +
		"场景布局与关键道具外观也要与对应参考图一致；" +
		"参考图只锁定身份，画面构图以首帧为准。\n\n")
	return b.String()
}

// buildVoiceSection 本片段说话人音色段（qai video-clips.ts buildVoiceSection 原文）。
// speakers 按首次开口顺序；voices 是全剧固定音色表。没有可注入的音色时返回空串。
func buildVoiceSection(speakers []string, voices []VoiceAsset) string {
	byName := make(map[string]VoiceAsset, len(voices))
	for _, voice := range voices {
		byName[voice.Name] = voice
	}
	var lines []string
	for _, name := range speakers {
		if voice, ok := byName[name]; ok && voice.Timbre != "" {
			lines = append(lines, "- "+voice.Name+"："+voice.Timbre)
		}
	}
	if len(lines) == 0 {
		return ""
	}
	return "【本片段说话人音色设定】\n" +
		strings.Join(lines, "\n") +
		"\n以上是这些角色全剧固定的嗓音，本片段配音必须严格照此发声；" +
		"情绪只改变语气、语速和音量，不得改变音色本身；不要给未列出的角色配台词。\n\n"
}

// composeVideoPrompt 拼出视频最终 prompt（qai buildKlingPrompt 结构）：
// 首帧锁定前缀 + 参考图逐图对应段 + 说话人音色段 + 全剧风格段 +
// 分镜正文（不摘要、不改写）。refSection 由 buildVideoRefSection 生成。
func composeVideoPrompt(storyboardText, refSection, voiceSection, styleAnchor string) string {
	var b strings.Builder
	b.WriteString(videoPromptPrefix)
	b.WriteString(refSection)
	b.WriteString(voiceSection)
	if styleAnchor != "" {
		b.WriteString("【全剧统一视觉风格】\n")
		b.WriteString(styleAnchor)
		b.WriteString("\n画面必须与首帧及上述风格保持一致。\n\n")
	}
	b.WriteString(storyboardText)
	b.WriteString("\n\n台词只生成为语音，画面中不得出现任何字幕、文字、对话框或水印。无背景音乐。")
	return b.String()
}

/* ---------- 各阶段 textgen 的 outputSchema ---------- */

func scriptSchema() []byte {
	return []byte(`{
		"type": "object",
		"required": ["title", "script"],
		"properties": {
			"title": {"type": "string"},
			"synopsis": {"type": "string"},
			"script": {"type": "string"}
		}
	}`)
}

func costumeSchema() []byte {
	return []byte(`{
		"type": "object",
		"required": ["costumes"],
		"properties": {
			"costumes": {
				"type": "array",
				"items": {
					"type": "object",
					"required": ["name", "prompt"],
					"properties": {
						"name": {"type": "string"},
						"variant": {"type": "string"},
						"prompt": {"type": "string"}
					}
				}
			},
			"voices": {
				"type": "array",
				"items": {
					"type": "object",
					"required": ["name", "timbre"],
					"properties": {
						"name": {"type": "string"},
						"gender": {"type": "string"},
						"age": {"type": "string"},
						"timbre": {"type": "string"}
					}
				}
			}
		}
	}`)
}

func splitSchema() []byte {
	return []byte(`{
		"type": "object",
		"required": ["episodes"],
		"properties": {
			"episodes": {
				"type": "array",
				"items": {
					"type": "object",
					"required": ["ep", "content"],
					"properties": {
						"ep": {"type": "integer"},
						"title": {"type": "string"},
						"content": {"type": "string"}
					}
				}
			}
		}
	}`)
}

func assetDescSchema() []byte {
	return []byte(`{
		"type": "object",
		"required": ["assets"],
		"properties": {
			"assets": {
				"type": "array",
				"items": {
					"type": "object",
					"required": ["category", "name", "prompt"],
					"properties": {
						"category": {"type": "string"},
						"name": {"type": "string"},
						"character_ref": {"type": ["string", "null"]},
						"prompt": {"type": "string"},
						"reuse_from_ep": {"type": ["integer", "null"]},
						"reuse_asset_name": {"type": ["string", "null"]}
					}
				}
			}
		}
	}`)
}

func simplifySchema() []byte {
	return []byte(`{
		"type": "object",
		"required": ["script"],
		"properties": {
			"script": {"type": "string"}
		}
	}`)
}

func storyboardSchema() []byte {
	return []byte(`{
		"type": "object",
		"required": ["shots"],
		"properties": {
			"shots": {
				"type": "array",
				"items": {
					"type": "object",
					"required": ["shot_id", "duration_s", "first_frame", "storyboard"],
					"properties": {
						"shot_id": {"type": "string"},
						"duration_s": {"type": "integer"},
						"characters": {"type": "array", "items": {"type": "string"}},
						"scenes": {"type": "array", "items": {"type": "string"}},
						"props": {"type": "array", "items": {"type": "string"}},
						"first_frame": {"type": "string"},
						"storyboard": {"type": "string"},
						"speakers": {"type": "array", "items": {"type": "string"}}
					}
				}
			}
		}
	}`)
}
