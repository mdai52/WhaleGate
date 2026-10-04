#!/usr/bin/env node
/**
 * 守卫：a-form 一旦使用 @finish，就必须带 :model。
 *
 * ant-design-vue 的 Form.handleSubmit 实现是：
 *
 *     const handleSubmit = e => {
 *       e.preventDefault(); e.stopPropagation(); emit('submit', e)
 *       if (props.model) {                      // ← 没有 model 就整段跳过
 *         validateFields().then(values => emit('finish', values))
 *       }
 *     }
 *
 * 也就是说缺了 :model 时 @finish 永远不会触发、也不会报错：
 * 提交按钮点下去毫无反应，用户看不到任何提示。
 * 这类问题 tsc 与 vite build 都发现不了，只能在 CI 拦住。
 */
import { readdirSync, readFileSync, statSync } from 'node:fs'
import { join, relative } from 'node:path'
import { fileURLToPath } from 'node:url'

const ROOT = fileURLToPath(new URL('..', import.meta.url))
const SRC = join(ROOT, 'src')

function walk(dir) {
  const out = []
  for (const name of readdirSync(dir)) {
    const full = join(dir, name)
    if (statSync(full).isDirectory()) {
      out.push(...walk(full))
    } else if (name.endsWith('.vue')) {
      out.push(full)
    }
  }
  return out
}

/** 取出所有 <a-form ...> 开标签（允许跨行），并排除 <a-form-item> / <a-form-xxx>。 */
function* aFormTags(source) {
  const re = /<a-form(?![-\w])[^>]*>/g
  let match
  while ((match = re.exec(source)) !== null) {
    yield { tag: match[0], index: match.index }
  }
}

const problems = []
for (const file of walk(SRC)) {
  const source = readFileSync(file, 'utf8')
  for (const { tag, index } of aFormTags(source)) {
    if (!/@finish/.test(tag) || /:model/.test(tag)) {
      continue
    }
    const line = source.slice(0, index).split('\n').length
    problems.push(`${relative(ROOT, file)}:${line}`)
  }
}

if (problems.length > 0) {
  console.error('✗ 以下 a-form 使用了 @finish 但缺少 :model，提交会静默失效：')
  for (const item of problems) {
    console.error(`    ${item}`)
  }
  console.error('\n修复：给该 a-form 补上 :model="<表单数据对象>"，例如 <a-form :model="form" @finish="submit">')
  process.exit(1)
}

console.log('✓ 表单提交守卫通过：所有使用 @finish 的 a-form 都带有 :model')
