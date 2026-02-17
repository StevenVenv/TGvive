📘 TGvive 前端 UI/UX 行为规范 (Design System)
1. 核心视觉风格 (Visual Language)
主题模式 (Theme): 强制 深色模式 (Dark Mode)。

背景色: #1e1e1e (主背景), #252525 (卡片/容器背景).

主色调: 青绿色 (参考宝塔/微信绿, e.g., #20a53a 或 #07c160)，用于主按钮、激活状态、成功提示。

危险色: #f56c6c (删除、停止、报错).

警告色: #e6a23c (暂停、受限).

文本色: #ffffff (主要), #a6a9ad (次要/Label), #606266 (占位符).

图标系统 (Iconography):

严禁使用 Emoji。

统一使用 RemixIcon (class="ri-...")。

常规状态使用线性图标 (-line)，激活/强调状态使用填充图标 (-fill)。

容器质感:

所有内容必须包裹在 ElCard 或自定义的 .card-box 中。

圆角: border-radius: 4px (微圆角，保持硬朗的工具感)。

边框: border: 1px solid #363637 (极细微的深灰边框)。

2. 布局规范 (Layout Patterns)
2.1 页面结构

侧边栏 (Sidebar): 固定左侧，宽度 200px - 220px。

内容区域 (Main):

全宽列表页 (List Page): (如账号管理) 表格占据 100% 宽度。

表单页 (Form Page): (如新建策略)

必须设置 max-width: 1200px。

必须 margin: 0 auto 居中显示。

禁止输入框横跨 2560px 的超宽屏幕。

2.2 栅格与密度 (Grid & Density)

高密度排版: 作为生产力工具，一屏应展示更多信息。

表单栅格 (Form Grid):

短字段 (延时、配额、端口): 一行 4 个 (span=6)。

中字段 (账号、开关组): 一行 2 个 (span=12)。

长字段 (URL、备注): 一行 1 个 (span=24)。

开关组 (Switch Group):

禁止每个开关独占一行。

使用 Flex 布局 (display: flex; gap: 20px; flex-wrap: wrap) 横向紧凑排列。

3. 组件交互规范 (Component Behavior)
3.1 下拉选择框 (Select)

富文本选项: 当选项包含图片（如头像）时，必须使用 Flex 布局对齐。

回显: 输入框内必须清晰显示选中的 Label（白色高亮），Placeholder 需为深灰色。

3.2 按钮 (Buttons)

主操作 (新建、保存): type="primary" (绿色)。

次操作 (取消、重置): type="default" (深灰背景，白字)。

危险操作 (删除、停止): type="danger" (红色)，且必须伴随二次确认弹窗 (ElMessageBox.confirm)。

表格内操作:

优先使用 文字链接 (type="primary" link)，以 | 分隔。

例如: 编辑 | 日志 | 删除。

3.3 弹窗与抽屉 (Dialog vs Drawer)

Dialog (对话框): 用于快速的新建、编辑（如“修改任务”、“编辑策略”）。

宽度: 600px - 800px。

必须包含 取 消 和 确 定 按钮。

Page / Tab (独立页面): 用于复杂的创建流程（如“新建策略模版”）。

给予全屏空间，便于未来扩展调试/预览功能。

Drawer (抽屉): 仅用于查看只读信息（如“查看实时日志”）。

3.4 实时反馈 (Feedback)

加载中 (Loading):

表格加载: v-loading 覆盖表格区域。

按钮提交: 点击后按钮必须进入 loading 状态，并禁用点击 (disabled)。

结果提示:

成功: ElMessage.success('操作成功')。

失败: ElMessage.error('错误原因') (直接显示后端返回的 msg)。

4. 模块特定规范 (Module Specifics)
4.1 任务管理 (Task Manager)

创建任务: 必须极简。仅保留 Source, Target, Account, Strategy 四个核心项。

日志查看: 推荐使用 右侧分栏 或 抽屉，背景必须为终端黑 (#000)，字体为绿色等宽字体 (Consolas).

4.2 策略管理 (Strategy Manager)

新建 vs 编辑:

新建: 在独立的 Tab 页中进行，全宽布局（带 max-width 限制）。

编辑: 在 Dialog 弹窗中进行（复用表单组件），方便快速微调。