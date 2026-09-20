/**
 * 导入结果的文案。
 *
 * 预检会告诉用户「共 N 行，有效 X 行，错误 Y 行」，所以导入结束时只报一个
 * 「成功写入 N 条」是会打架的——以前后端把预检判为错误的行照样写进去，于是屏幕上
 * 先后出现「有效 1 行、错误 10 行」和「成功写入 11 条」。现在后端按同一套规则跳过
 * 那些行，这里就得把跳过数一并说出来，两条消息才对得上。
 *
 * 纯函数，便于单测钉住「跳过不为 0 时必须出现在文案里」。
 */
export function importResultCopy(inserted: number, skipped = 0): string {
  const tail = '当前状态按 CSV 记录，不伪造历史。'
  if (skipped > 0) {
    return `导入完成：成功写入 ${inserted} 条，跳过 ${skipped} 条（预检里标红的行）。${tail}`
  }
  return `导入完成：成功写入 ${inserted} 条。${tail}`
}

/**
 * 预检结果的文案。
 *
 * 「疑似重复」现在是**和库里已有岗位**比对的结果（以前只在文件内部两两比较，于是
 * 把自己导出的 CSV 原样导回去会报「疑似重复 0 行」然后把每个岗位翻一倍）。既然
 * 重复项默认新建、不覆盖，这句话就必须把后果说清楚：这 N 行会变成第二条记录。
 *
 * 纯函数，便于单测钉住「有重复时必须说出后果」。
 */
export function importPreviewCopy(
  total: number,
  valid: number,
  errors: number,
  duplicates: number,
): string {
  let msg = `预检完成：共 ${total} 行，有效 ${valid} 行，错误 ${errors} 行，疑似重复 ${duplicates} 行。`
  if (duplicates > 0) {
    msg += ` 重复的行不会覆盖已有岗位，确认导入后会再建 ${duplicates} 条新记录——不想要就先从 CSV 里删掉这些行。`
  }
  if (errors > 0) {
    msg += ' 错误的行会被跳过，可展开逐行报告或修正 CSV 后重试。'
  }
  return msg
}
