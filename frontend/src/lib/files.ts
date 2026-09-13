/** 附件类别：与后端 files.category 白名单一致（backend internal/files/transport/http.go）。 */
export const FILE_CATEGORIES: Array<{ key: string; label: string }> = [
  { key: 'resume', label: '简历' },
  { key: 'cover_letter', label: '求职信' },
  { key: 'portfolio', label: '作品集' },
  { key: 'other', label: '其它' },
]

export function categoryLabel(key: string): string {
  return FILE_CATEGORIES.find((c) => c.key === key)?.label ?? '其它'
}

/** 按文件名猜类别；猜不出回退 resume。仅用于上传前的初始选中，行内可随时改。 */
export function guessCategory(name: string): string {
  if (/cover[_ -]?letter|求职信/i.test(name)) return 'cover_letter'
  if (/portfolio|作品集/i.test(name)) return 'portfolio'
  if (/jd|职位描述|岗位描述/i.test(name)) return 'other'
  return 'resume'
}
