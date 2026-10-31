/** BuyerBlock 是黑名单编辑草稿，不包含本地用户标识或账号凭据。 */
export interface BuyerBlock {
  /** id 为零时表示尚未保存的新记录。 */
  id: number;
  /** cookie_id 为空表示当前用户全部账号。 */
  cookie_id: string;
  /** item_id 为空表示全部商品，非空时必须限定账号。 */
  item_id: string;
  /** buyer_id 是平台稳定买家 ID，不能使用昵称。 */
  buyer_id: string;
  /** buyer_nick 只供显示，不参与交易匹配。 */
  buyer_nick?: string;
  /** reason 是仅供管理者查看的备注。 */
  reason: string;
  /** enabled 控制是否参与交易拦截。 */
  enabled: boolean;
  /** auto_close 显式授权系统事件命中时尝试一次关单。 */
  auto_close: boolean;
}
