export type MemberPlan = { code: string; tier: string; name: string; durationDays: number; priceCents: number; currency: string };

export function money(cents: number, currency = "CNY") {
  return new Intl.NumberFormat("zh-CN", { style: "currency", currency, minimumFractionDigits: 0, maximumFractionDigits: 2 }).format(cents / 100);
}

export function PlanList({ plans, selected, onSelect }: {
  plans: MemberPlan[];
  selected?: string;
  onSelect?: (code: string) => void;
}) {
  return <div className="member-tiers">{["standard", "premium"].map(tier => {
    const items = plans.filter(p => p.tier === tier);
    if (!items.length) return null;
    return <fieldset key={tier}><legend>{tier === "premium" ? "豪华会员" : "普通会员"}</legend>
      <div className="member-choices">{items.map(p => {
        const content = <><span>{p.durationDays} 天</span><strong>{money(p.priceCents, p.currency)}</strong></>;
        return onSelect ? <label key={p.code} className="member-choice">
          <input type="radio" name="plan" checked={selected === p.code} onChange={() => onSelect(p.code)} />
          {content}</label>
          : <a className="member-choice" key={p.code} href={"/account?plan=" + encodeURIComponent(p.code)} aria-label={p.name + " " + money(p.priceCents, p.currency)}>{content}</a>;
      })}</div>
    </fieldset>;
  })}</div>;
}
