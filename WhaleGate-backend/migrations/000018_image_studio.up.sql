-- 绘图工作台：提示词词库。
CREATE TABLE IF NOT EXISTS prompt_templates (
    id            BIGSERIAL PRIMARY KEY,
    category      VARCHAR(32)  NOT NULL,
    title         VARCHAR(128) NOT NULL,
    summary       VARCHAR(255) NOT NULL DEFAULT '',
    prompt        TEXT         NOT NULL,
    tags          TEXT         NOT NULL DEFAULT '[]',
    model_hint    VARCHAR(64)  NOT NULL DEFAULT '',
    aspect_ratio  VARCHAR(16)  NOT NULL DEFAULT '',
    sort          INTEGER      NOT NULL DEFAULT 0,
    enabled       BOOLEAN      NOT NULL DEFAULT TRUE,
    created_at    TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_prompt_templates_category ON prompt_templates (category);
CREATE INDEX IF NOT EXISTS idx_prompt_templates_enabled ON prompt_templates (enabled, sort DESC);

COMMENT ON TABLE prompt_templates IS '绘图工作台提示词词库';
COMMENT ON COLUMN prompt_templates.category IS '分类键：portrait / product / poster / infographic / design / illustration / photo / editing / typography / brand';
COMMENT ON COLUMN prompt_templates.prompt IS '提示词正文，可直接送入生图接口';
COMMENT ON COLUMN prompt_templates.tags IS '标签 JSON 数组';
COMMENT ON COLUMN prompt_templates.model_hint IS '推荐模型名，仅作表单默认值';
COMMENT ON COLUMN prompt_templates.aspect_ratio IS '推荐画幅，如 1:1 / 9:16 / 16:9';

-- 绘图工作台：生成作品。图片以本地文件托管，仅在上传失败时保留上游临时地址。
CREATE TABLE IF NOT EXISTS image_works (
    id            BIGSERIAL PRIMARY KEY,
    user_id       BIGINT       NOT NULL,
    api_key_id    BIGINT       NOT NULL DEFAULT 0,
    channel_id    BIGINT       NOT NULL DEFAULT 0,
    model         VARCHAR(128) NOT NULL,
    prompt        TEXT         NOT NULL,
    params        TEXT         NOT NULL DEFAULT '{}',
    images        TEXT         NOT NULL DEFAULT '[]',
    status        SMALLINT     NOT NULL DEFAULT 1,
    error_message TEXT         NOT NULL DEFAULT '',
    image_count   INTEGER      NOT NULL DEFAULT 0,
    latency_ms    INTEGER      NOT NULL DEFAULT 0,
    source        VARCHAR(16)  NOT NULL DEFAULT 'studio',
    created_at    TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_image_works_user ON image_works (user_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_image_works_status ON image_works (status);
CREATE INDEX IF NOT EXISTS idx_image_works_channel ON image_works (channel_id);

COMMENT ON TABLE image_works IS '绘图工作台生成作品';
COMMENT ON COLUMN image_works.params IS '生成参数 JSON 对象：size / n / quality / aspect_ratio 等';
COMMENT ON COLUMN image_works.images IS '生成结果 JSON 数组：[{file,url,mime_type,revised_prompt}]';
COMMENT ON COLUMN image_works.status IS '1 成功 / 2 失败';
COMMENT ON COLUMN image_works.image_count IS '成功生成的图片张数';
COMMENT ON COLUMN image_works.source IS '来源：studio 工作台 / api 接口调用';

-- 词库种子数据：按分类各收录两条可直接使用的模板。重复执行时按主键跳过。
INSERT INTO prompt_templates (id, category, title, summary, prompt, tags, model_hint, aspect_ratio, sort) VALUES
(1, 'portrait', '高级定制人像', '柔和自然光下的室内人像，皮肤质感细腻', $prompt$室内自然光人像摄影，一位亚洲女性身着米色羊绒大衣侧坐窗边，午后柔光从百叶窗斜射进来，浅景深虚化背景，皮肤质感细腻可见毛孔，眼神平静看向镜头外。中长焦镜头 f/1.8，胶片色调，克制的暖调分级，画面留白充足。$prompt$, '["人像","自然光","胶片"]'::text, 'gpt-image-2', '3:4', 100),
(2, 'portrait', '赛博霓虹夜景人像', '雨夜霓虹街道的青橙双色人像', $prompt$雨夜霓虹街头人像，人物身穿黑色机能风外套站在便利店门口，背后是青蓝与品红交织的霓虹灯牌，地面湿漉漉反射光斑与雨丝，浅景深虚化光点形成圆形光斑，电影感青橙双色调分级，高对比低饱和，85mm 定焦。$prompt$, '["人像","赛博","夜景"]'::text, 'gpt-image-2', '3:4', 90),

(3, 'product', '玫瑰天鹅绒草莓奶昔', '奢华商业摄影质感的产品广告图', $prompt$商品摄影：一瓶玫瑰色草莓奶昔置于白色大理石台面，瓶身丝绸标签压印「ROSE VELVET」，周围散落新鲜草莓与玫瑰花瓣，背景是粉白渐变的绒面幕布，侧逆光勾勒瓶身高光与冷凝水珠，影棚柔光箱布光，微距质感，色彩高级不艳俗，1:1 构图居中，国际美妆广告级修图。$prompt$, '["商品","电商","美妆"]'::text, 'gpt-image-2', '1:1', 100),
(4, 'product', '3D 冰霜罐装饮料', '九宫格产品色板，一次生成多款口味', $prompt$四罐铝制饮料整齐排成两行两列，罐身结满白霜，桌面铺浅灰纯色背景并投下柔和的椭圆阴影。每罐主色不同：青柠绿、蜜桃橙、蓝莓紫、原味银，字体为无衬线粗体大写。整体为高质量 C4D 渲染，金属反射与霜面细节清晰，顶光拍摄，画面干净适合电商主图，1:1。$prompt$, '["商品","3D","饮料"]'::text, 'gpt-image-2', '1:1', 95),

(5, 'poster', '极简杂志封面', '大面积留白的高级感杂志版式', $prompt$极简风格杂志封面设计，A4 竖版。画面主体是一支半开的白色郁金香，位于版面上方三分之一处，下方大面积奶油色留白用于排字。标题使用衬线字体大写，字距舒展，副标题小字对齐左边缘，右下角有细线分割的期号。整体配色仅奶油白、灰绿与一点暗金，纸质颗粒质感，印刷级版式。$prompt$, '["海报","版式","极简"]'::text, 'gpt-image-2', '3:4', 100),
(6, 'poster', '电影感三联海报', '同一主题的三格叙事海报', $prompt$电影海报三联画，三格等宽竖排构成一个完整故事：左格是清晨空荡的公交站台，中格是车内握紧扶手的手部特写，右格是黄昏时分车窗外的城市剪影。三格色调统一为低饱和青灰，中间有细白分割线，底部横贯一行小号无衬线中文标题与英文副标题，颗粒感与轻微暗角，文艺片气质。$prompt$, '["海报","电影感","三联"]'::text, 'gpt-image-2', '16:9', 90),

(7, 'infographic', '知识科普信息图', '结构清晰的中文科普长图', $prompt$中文知识信息图，竖版长图风格。标题居中大号加粗，下方分为三个等高的横向模块，每个模块左侧是一个扁平线性图标（咖啡杯、大脑、时钟），右侧是两行说明文字与一行小字数据。整体配色为浅米底 + 深墨绿主色 + 一点赭石强调色，模块之间用细线分隔，留白充足，字体统一为思源黑体感，画面整洁适合公众号配图。$prompt$, '["信息图","科普","中文"]'::text, 'gpt-image-2', '3:4', 100),
(8, 'infographic', '步骤教程图解', '分步操作的流程示意图', $prompt$分步骤操作图解，四步横向流程。每一步用一个圆角矩形卡片表示，卡片内上方是简笔插画（拆包装、倒入杯中、加水搅拌、成品举起），下方一行中文短句说明，四张卡片之间用箭头连接。整体为扁平插画风格，主色为暖橙与浅蓝，背景纯白，线条粗细统一，字号层级分明，适合作为教程首图。$prompt$, '["信息图","教程","流程"]'::text, 'gpt-image-2', '16:9', 90),

(9, 'design', 'SaaS 后台界面概念稿', '现代后台控制台的 UI 效果图', $prompt$现代 SaaS 后台控制台界面设计稿，桌面端 16:10 比例。左侧固定窄边栏含五个线性图标，顶部是搜索框与头像，主区域上方三张等宽数据卡片（大号数字 + 迷你折线），下方是一张宽表格与右侧环形图表。配色为深藏青主色 #023B82 搭配浅灰背景，圆角 8px，卡片带极轻投影，玻璃拟态细节克制，界面文字为中文占位符，整体干净专业。$prompt$, '["UI","后台","SaaS"]'::text, 'gpt-image-2', '16:9', 100),
(10, 'design', '移动端产品页', 'App 落地页的三屏展示', $prompt$移动端 App 产品页展示，三台手机并排斜向错位排列，屏幕内分别是登录页、首页信息流与个人中心。界面风格为浅色卡片式，主色亮蓝，圆角大、留白多，图标为线性圆润风格。手机外框为细边框哑光金属，背景是从浅蓝到白的柔和渐变并带几颗半透明圆形装饰，整体为产品宣传图风格，光影柔和，画面横向延展。$prompt$, '["UI","移动端","产品页"]'::text, 'gpt-image-2', '16:9', 95),

(11, 'illustration', '吉卜力风格插画', '日式手绘动画场景', $prompt$日式手绘动画风格插画，一片开满小野花的夏日草坡，远处是白云堆积的蓝天与一座红顶小屋，一个穿白裙的女孩背对画面站在坡顶，裙摆被风吹起。笔触细腻，色彩明快而柔和，天空有手绘水彩的渐变层次，草叶有细密的高光，整体氛围安静治愈，宫崎骏动画电影质感，横构图。$prompt$, '["插画","动画","治愈"]'::text, 'gpt-image-2', '16:9', 100),
(12, 'illustration', '3D 卡通角色', '皮克斯风格的拟人动物角色', $prompt$3D 渲染卡通角色，一只拟人化的橘猫快递员，戴深蓝色鸭舌帽，穿工装短背心，正抱着一只牛皮纸包裹小跑，表情认真又有点得意。皮克斯风格的大眼睛与圆润造型，毛发有细软的质感与次表面散射，背景是虚化的清晨街道，暖色晨光从右后方打来形成轮廓光，景深浅，画面明亮友好。$prompt$, '["3D","角色","卡通"]'::text, 'gpt-image-2', '1:1', 95),

(13, 'photo', '真实摄影肖像', 'iPhone 原相机质感的人像', $prompt$写实人像摄影，iPhone 原相机拍摄质感。一位穿灰色连帽卫衣的年轻人在城市天桥上回望，自然光无明显补光，皮肤保留真实纹理与轻微噪点，衣服布料褶皱自然垂落，背景是虚化的交通灯与行人。竖版 9:16，中心对称构图，相机略微偏下仰拍，色调接近原片不做过饱和调色，风格为真实街拍记录。$prompt$, '["摄影","人像","写实"]'::text, 'gpt-image-2', '9:16', 100),
(14, 'photo', '街头纪实摄影', '高对比黑白街头瞬间', $prompt$黑白街头纪实摄影，一个撑伞的行人从地铁站出口逆光走出，背后是甬道口涌出的强光形成剪影，雨天地面湿润反射出模糊的人影与灯光。高对比度，暗部保留细节，颗粒感明显，构图使用大面积负空间，抓拍的瞬间感强烈，35mm 焦段，街头摄影师视角。$prompt$, '["摄影","纪实","黑白"]'::text, 'gpt-image-2', '3:2', 90),

(15, 'editing', '老照片修复上色', '在保留原貌的前提下修复与上色', $prompt$影像修复任务：把这张老旧黑白家庭合影修复为清晰彩色照片。要求保留人物原本的面部特征、姿态与构图完全不变；去除折痕、噪点与霉斑，恢复被磨损的细节；上色采用自然克制的肤色与 1970 年代服装色调，不要过度饱和度；整体保持胶片颗粒质感而非过度锐化的数码感。$prompt$, '["修图","修复","上色"]'::text, 'gpt-image-2', '4:3', 100),
(16, 'editing', '商品背景替换', '把实拍商品换成纯色影棚背景', $prompt$商品抠图与背景替换：保留主体商品（包括边缘透明部分与投影）不做任何形变，将背景替换为纯净的浅灰影棚背景，底部添加自然的椭圆接触阴影。整体光线方向保持与原图一致，商品边缘需要清理干净无彩色描边，最终输出适合电商主图的规整构图，1:1 比例，四角留出安全边距。$prompt$, '["修图","电商","抠图"]'::text, 'gpt-image-2', '1:1', 90),

(17, 'typography', '金属质感 3D 字效', '镀铬立体文字的渲染效果', $prompt$3D 文字效果：单词「GATE」以粗体几何无衬线大写呈现，字面为抛光镀铬金属材质，表面有清晰的环境反射与高光，字形带厚度的立体倒角。文字悬浮在深色渐变背景前，下方投射柔和的彩色反射光斑，冷蓝与紫红补光从两侧打入，整体为高质量 C4D + Octane 渲染，中心构图，适合作为视觉主标题。$prompt$, '["字效","3D","金属"]'::text, 'gpt-image-2', '16:9', 100),
(18, 'typography', '玻璃糖果 3D 字效', '半透明彩色玻璃质感的立体字', $prompt$3D 文字效果：单词「SWEET」使用圆润饱满的无衬线字形，材质为半透明彩色玻璃糖果，四种颜色渐变过渡（草莓粉、蜜桃橙、薄荷绿、葡萄紫），内部有气泡与折射，边缘有柔和的高光带。文字浮在浅色纯背景上，下方是模糊的彩色透射阴影与光斑，整体通透轻盈，商业包装级渲染，1:1 构图。$prompt$, '["字效","3D","玻璃"]'::text, 'gpt-image-2', '1:1', 95),

(19, 'brand', '极简品牌标识', '几何构成的标志设计与应用', $prompt$品牌标识设计：为一个名为「鲸闸 WhaleGate」的基础设施产品设计极简标志。图形由一条抽象的鲸尾与一道闸门横杆组合而成，用两笔几何线条概括，负空间留白干净，线条末端为圆角。下方是中文与英文标准字，中文字体为现代黑体，英文为几何无衬线小写。整体仅用单一深藏青色 #023B82，背景纯白，展示为居中的标志规范图。$prompt$, '["Logo","品牌","极简"]'::text, 'gpt-image-2', '1:1', 100),
(20, 'brand', '品牌视觉套件', '标志与周边物料的组合展示', $prompt$品牌视觉套件展示图：同一套标志应用到六件物料上并斜向排列——名片、牛皮纸手提袋、圆形徽章、纸杯、信封与帆布包。所有物料保持统一的深藏青 + 米白双色体系，标志在不同材质上以烫金或丝印效果呈现。整体为俯拍平铺构图，浅灰背景，柔和均匀的顶光，物料之间有轻微重叠与自然投影，画面整洁有秩序感，适合作为品牌提案页。$prompt$, '["品牌","物料","提案"]'::text, 'gpt-image-2', '1:1', 95)
ON CONFLICT (id) DO NOTHING;

SELECT setval(pg_get_serial_sequence('prompt_templates', 'id'),
              GREATEST((SELECT COALESCE(MAX(id), 1) FROM prompt_templates), 1));
