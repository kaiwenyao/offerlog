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
