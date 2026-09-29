# 学习反馈内容：标注与审核规则

状态：草案，待固定内部审核人验收。候选题量与来源摘要见 `products/quizcraft/generated/learning_feedback_inventory.json`；不能据此认定课程可发布。

## 分类

- 知识点：课程内可解释、可练习的概念或方法，记录稳定 ID、名称、定义；章节是组织维度，不能冒充知识点。
- 能力题型：概念辨析、代码追踪等被测能力，不是 single/multi/judge/blank。例子说明分类，不预设课程已采用。
- 题目可多标签，但只标实际考查内容；绑定题目及版本，不凭相似题干或 reviewed 文件名继承审核。
- AI 只产候选标注；人工抽检覆盖拟开放标签与作答形式。未解决错标/能力夸大阻止相关范围开放。

## 内容包

首版按稳定 bank_id 保存一个可开启的课程/题库范围，不自动合并题库。版本化内容包包含标签、题目版本映射、资料来源和预生成讲解。

- 来源：HENU-Final-Review 具体文件/章节、版本或 commit、摘要、定位和使用状态；只有仓库 URL 不足。
- 讲解：学习目标、解释、示例/易错点、引用；拟发布讲解逐条人审，不能拿原题 analysis 冒充已审教材。
- 练习：只引用可访问且已发布的现有题；缺题标缺口，不自动补题。
- 审核由受权服务端 actor 留痕；导入 JSON 不得自报 approved 或冒充审核人。
- draft → approved → retired；修正产生新版本，撤回使关联报告过期，旧建议不能启动练习。

## 报告评测

人工验收充分证据、单题重复、偶发错题、冲突、未覆盖、疑似错题等代表性样本。引用真实与推断正确分开判断，不把题库正确率称为能力概率。

编造事实、越权、确定式错误归因或错误讲解阻止相关范围开放；不是每份线上报告等人工审批。

## 复现

```bash
python3 -m unittest discover -s products/quizcraft/tests -p test_learning_feedback_inventory.py
python3 products/quizcraft/scripts/learning_feedback_inventory.py --check
```

盘点不授予发布许可。真实资料核验、标签、讲解和人工审核记录尚未完成。

## 已实现的内容包格式与边界

`products/quizcraft/go-service/learning_content.go` 提供 `ParseLearningContent`，输入只能是一个 JSON 草稿，最多 4 MiB；调用方必须从当前已发布题库版本加载权威题目成员映射，不接受客户端自报范围。

- 顶层：`schema_version: 1`、`tags`、`questions`、`sources`、`lessons`；未知字段（包括 `status` / `reviewed_by`）拒绝。
- 标签：稳定 `id`、`kind: knowledge | ability`、`name`、`definition`；最多 300 个。
- 题目：`question_id`、`question_version_id`、`tag_ids`；最多 10000 题，须匹配当前发布版本，不能重号或引用不存在的标签。
- 来源：`id`、`title`、固定 `repository`、40 位 commit、仓库内 `path`、文件 `sha256`、`locator`、`usage_basis`；最多 1000 项。分支名与路径穿越不能作为可追溯来源。
- 讲解：`id`、`title`、`body`、`tag_ids`、`source_ids`；最多 300 条，正文最多 10000 字符；每条引用 1–16 个有效且不重复的标签/来源。
- 没有讲解可保留空列表并记录缺口；不能从题目解析或即时生成内容兜底。结构检查只确认声明齐全，不验证来源实际存在、版权许可或语义正确。

摘要基于规范化 JSON，排版差异不产生新摘要。解析成功仅代表可保存草稿；发布仍须服务端审核记录、当前版本、目录门禁及独立人工验收。测试中的资料/讲解均为合成样本，不可当作真实审核内容。
