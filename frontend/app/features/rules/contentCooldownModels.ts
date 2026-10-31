/** ContentCooldownReview 为 UI 定位待核对消息，不保存内容正文或摘要键。 */
export interface ContentCooldownReview {
  /** id、version 是明确解除时使用的旧保护身份。 */
  id: number;
  /** version 防止旧页面解除并发新保护。 */
  version: number;
  /** chatID 是本人账号下的会话标识。 */
  chatID: string;
  /** messageKey 是原买家消息的平台标识。 */
  messageKey: string;
  /** claimedAt 是本地接纳消息的 Unix 毫秒。 */
  claimedAt: number;
}

/** ContentCooldownPage 为本地待核对分页，不包含平台访问状态。 */
export interface ContentCooldownPage {
  /** items 是稳定主键排序后的当前页。 */
  items: ContentCooldownReview[];
  /** total 是本人当前账号的全部待核对数量。 */
  total: number;
  /** limit、offset 为有界页长和已跳过记录数。 */
  limit: number;
  /** offset 是当前页位置。 */
  offset: number;
}
