-- api_keys.masked_key 是纯展示字段。
-- 旧实现的掩码只隐藏末 4 位，列表里展示的字符串几乎等于真实密钥，
-- 用户很容易误把它当成完整密钥复制去调用，结果得到「凭证无效」的 401。
--
-- 这里把存量记录统一改成「前 6 位 + ... + 末 4 位」的形态，
-- 让界面上一眼能看出它不是完整密钥。
-- 旧格式中被星号覆盖的末 4 位无法还原，因此统一以 **** 占位。
--
-- 条件里的 NOT LIKE '%...%' 保证重复执行不会破坏已是新格式的记录。
UPDATE api_keys
SET masked_key = substring(masked_key from 1 for 6) || '...****'
WHERE masked_key LIKE '%****'
  AND masked_key NOT LIKE '%...%'
  AND length(masked_key) > 10;
