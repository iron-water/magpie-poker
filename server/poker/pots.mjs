// Pot construction and awarding. Inputs are plain objects so they survive JSON.
// A player: { id, seat, committed, folded }.

export function computePots(players) {
  const rows = players.map((p) => ({ id: p.id, committed: Math.max(0, Math.floor(p.committed || 0)), folded: !!p.folded }));
  const byId = new Map(rows.map((r) => [r.id, r]));
  const refunds = [];

  // An uncalled bet comes back before pots are built: only the amount matched by
  // somebody else can be won.
  const sorted = [...rows].sort((a, b) => b.committed - a.committed);
  const top = sorted[0]?.committed || 0;
  const second = sorted.length > 1 ? sorted[1].committed : 0;
  if (top > second && sorted[0]) {
    const r = byId.get(sorted[0].id);
    const back = top - second;
    r.committed -= back;
    refunds.push({ id: r.id, amount: back });
  }

  const levels = [...new Set(rows.map((r) => r.committed).filter((n) => n > 0))].sort((a, b) => a - b);
  const pots = [];
  let prev = 0;
  for (const level of levels) {
    let amount = 0;
    for (const r of rows) amount += Math.max(0, Math.min(r.committed, level) - prev);
    const eligible = rows.filter((r) => !r.folded && r.committed >= level).map((r) => r.id);
    if (amount > 0) {
      const last = pots[pots.length - 1];
      if (last && sameSet(last.eligible, eligible)) last.amount += amount;
      else pots.push({ amount, eligible });
    }
    prev = level;
  }
  return { pots, refunds, total: pots.reduce((n, p) => n + p.amount, 0) };
}

// scoreOf(id) -> comparable number. Splits evenly; odd chips go to the first
// winner left of the dealer. Returns one row per pot.
export function awardPots(pots, scoreOf, { dealerSeat, seatCount, seatOf }) {
  return pots.map((pot) => {
    if (!pot.eligible.length) return { ...pot, winners: [] };
    let best = -Infinity;
    for (const id of pot.eligible) best = Math.max(best, scoreOf(id));
    const winners = pot.eligible.filter((id) => scoreOf(id) === best);
    winners.sort((a, b) => clockwise(a, b, dealerSeat, seatCount, seatOf));
    const share = Math.floor(pot.amount / winners.length);
    let odd = pot.amount - share * winners.length;
    return {
      amount: pot.amount,
      eligible: pot.eligible,
      winners: winners.map((id) => ({ id, chips: share + (odd-- > 0 ? 1 : 0) })),
    };
  });
}

function sameSet(a, b) {
  if (a.length !== b.length) return false;
  const s = new Set(a);
  return b.every((x) => s.has(x));
}

function clockwise(a, b, dealerSeat, seatCount, seatOf) {
  const da = (seatOf(a) - dealerSeat - 1 + seatCount * 2) % seatCount;
  const db = (seatOf(b) - dealerSeat - 1 + seatCount * 2) % seatCount;
  return da - db;
}
