// SPDX-License-Identifier: Apache-2.0

package governance

import (
	"fmt"
	"math/rand"
	"testing"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	v1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1"
	"github.com/stretchr/testify/require"
)

const modelMinIdentityAge = 300 * time.Second

type modelDID struct {
	createdAt int64
	active    bool
}

type modelController struct {
	dids []modelDID

	hasRecord     bool
	oldest        int64
	eligibleSince int64
}

type modelRegistry struct {
	controllers map[string]*modelController
	now         *int64
}

func newModelRegistry(now *int64) *modelRegistry {
	return &modelRegistry{controllers: map[string]*modelController{}, now: now}
}

func (m *modelRegistry) refresh(c *modelController) {
	oldest, has := int64(0), false
	for _, d := range c.dids {
		if d.active && (!has || d.createdAt < oldest) {
			oldest, has = d.createdAt, true
		}
	}
	switch {
	case c.hasRecord && !has:
		c.hasRecord = false
	case !c.hasRecord && has:
		c.hasRecord, c.oldest, c.eligibleSince = true, oldest, *m.now
	case c.hasRecord && has:
		if oldest < c.oldest {
			c.eligibleSince = *m.now // the basis improved: standing restarts
		}
		c.oldest = oldest
	}
}

func (m *modelRegistry) register(addr string, createdAt int64) {
	c := &modelController{dids: []modelDID{{createdAt: createdAt, active: true}}}
	m.controllers[addr] = c
	m.refresh(c)
}

func (m *modelRegistry) suspend(addr string) {
	if c, ok := m.controllers[addr]; ok {
		for i := range c.dids {
			c.dids[i].active = false
		}
		m.refresh(c)
	}
}

func (m *modelRegistry) reinstate(addr string) {
	if c, ok := m.controllers[addr]; ok && !c.hasRecord {
		for i := range c.dids {
			c.dids[i].active = true
		}
		m.refresh(c)
	}
}

func (m *modelRegistry) recover(from, to string) bool {
	src, ok := m.controllers[from]
	dst, ok2 := m.controllers[to]
	if !ok || !ok2 || from == to {
		return false
	}
	best := -1
	for i, d := range src.dids {
		if d.active && (best < 0 || d.createdAt < src.dids[best].createdAt) {
			best = i
		}
	}
	if best < 0 {
		return false
	}
	moved := src.dids[best]
	src.dids = append(src.dids[:best:best], src.dids[best+1:]...)
	dst.dids = append(dst.dids, moved)
	m.refresh(src)
	m.refresh(dst)
	return true
}

func (m *modelRegistry) IsEligibleControllerAt(_ sdk.Context, controller string, t time.Time, minAge time.Duration) bool {
	c, ok := m.controllers[controller]
	return ok && c.hasRecord && c.oldest <= t.Add(-minAge).Unix()
}

func (m *modelRegistry) IsEligibleControllerSince(_ sdk.Context, controller string, t time.Time, minAge time.Duration, since time.Time) bool {
	c, ok := m.controllers[controller]
	return ok && c.hasRecord && c.oldest <= t.Add(-minAge).Unix() && c.eligibleSince <= since.Unix()
}

func (m *modelRegistry) CountEligibleControllersAt(_ sdk.Context, t time.Time, minAge time.Duration) uint64 {
	cutoff := t.Add(-minAge).Unix()
	var n uint64
	for _, c := range m.controllers {
		if c.hasRecord && c.oldest <= cutoff {
			n++
		}
	}
	return n
}

func (m *modelRegistry) EligibleControllerTotal(_ sdk.Context) uint64 {
	var n uint64
	for _, c := range m.controllers {
		if c.hasRecord {
			n++
		}
	}
	return n
}

func (m *modelRegistry) MinIdentityAge(_ sdk.Context) time.Duration { return modelMinIdentityAge }

func voterAddr(i int) sdk.AccAddress {
	b := make([]byte, 20)
	copy(b, fmt.Sprintf("voter-%013d", i))
	return sdk.AccAddress(b)
}

// TestNet_TurnoutIsAlwaysASubsetOfTheFrozenDenominator drives randomised sequences of registry transitions, freezes and ballots against the real hook and keeper, asserting the registered invariant after every single step.
func TestNet_TurnoutIsAlwaysASubsetOfTheFrozenDenominator(t *testing.T) {
	const (
		voters = 12
		steps  = 400
		seeds  = 40
	)

	for seed := int64(0); seed < seeds; seed++ {
		t.Run(fmt.Sprintf("seed-%d", seed), func(t *testing.T) {
			rng := rand.New(rand.NewSource(seed))
			ctx, k := govSetup(t)

			now := int64(2_000_000)
			ctx = ctx.WithBlockTime(time.Unix(now, 0))
			reg := newModelRegistry(&now)
			for i := 0; i < voters; i++ {
				reg.register(voterAddr(i).String(), now-int64(1+rng.Intn(1000)))
			}

			hooks := NewVoteHooks(k, nil, reg, publicRoutes{}, noValidators{})
			inv := TurnoutWithinFrozenBasisInvariant(k)

			proposals := []uint64{1, 2, 3}
			accepted, recoveries := 0, 0

			assertNet := func(step int, what string) {
				t.Helper()
				msg, broken := inv(ctx)
				require.False(t, broken, "step %d (%s): %s", step, what, msg)
			}

			advance := func() {
				now += int64(1 + rng.Intn(60))
				ctx = ctx.WithBlockTime(time.Unix(now, 0)).WithBlockHeight(ctx.BlockHeight() + 1)
			}

			for i := 0; i < voters; i++ {
				if rng.Intn(2) == 0 {
					reg.suspend(voterAddr(i).String())
				}
			}
			advance()

			for _, id := range proposals {
				hooks.freezeEligibilityOnce(ctx, proposalAt(id, now))
				advance()
			}
			assertNet(0, "freeze")

			for i := 0; i < voters; i++ {
				addr := voterAddr(i)
				for _, id := range proposals {
					if hooks.recordBallot(ctx, proposalAt(id, now), addr.String(), addr.Bytes(),
						single(randomOption(rng))) == nil {
						accepted++
					}
				}
			}
			assertNet(0, "opening round")

			for step := 1; step <= steps; step++ {
				switch rng.Intn(8) {
				case 0:
					advance()
					assertNet(step, "advance")

				case 1: // a controller loses its last active DID
					reg.suspend(voterAddr(rng.Intn(voters)).String())
					assertNet(step, "suspend")

				case 2, 3: // a controller regains one, mid-vote
					reg.reinstate(voterAddr(rng.Intn(voters)).String())
					assertNet(step, "reinstate")

				case 4: // a new controller onboards mid-flight
					reg.register(voterAddr(voters+rng.Intn(voters)).String(), now)
					assertNet(step, "register")

				case 5, 6: // social recovery moves an older DID onto another controller, mid-vote
					from := voterAddr(rng.Intn(voters + voters)).String()
					to := voterAddr(rng.Intn(voters + voters)).String()
					if reg.recover(from, to) {
						recoveries++
					}
					assertNet(step, "recover")

				case 7: // a ballot is cast
					id := proposals[rng.Intn(len(proposals))]
					addr := voterAddr(rng.Intn(voters + voters))
					if hooks.recordBallot(ctx, proposalAt(id, now), addr.String(), addr.Bytes(),
						single(randomOption(rng))) == nil {
						accepted++
					}
					assertNet(step, "vote")
				}
			}

			require.Positive(t, accepted, "the sequence never counted a single ballot")
			require.Positive(t, recoveries, "the sequence never moved a DID between controllers")
		})
	}
}

func randomOption(rng *rand.Rand) v1.VoteOption {
	return []v1.VoteOption{v1.OptionYes, v1.OptionNo, v1.OptionNoWithVeto, v1.OptionAbstain}[rng.Intn(4)]
}

func proposalAt(id uint64, t int64) v1.Proposal {
	start := time.Unix(t, 0)
	return v1.Proposal{Id: id, VotingStartTime: &start}
}

// A directed reproduction of the drift the net exists to forbid: a controller suspended BEFORE the freeze is absent from the denominator, and reinstating it mid-vote must not let it into the numerator.
func TestNet_SuspendedBeforeFreezeCannotReturnToTheNumerator(t *testing.T) {
	ctx, k := govSetup(t)
	now := int64(3_000_000)
	ctx = ctx.WithBlockTime(time.Unix(now, 0))

	reg := newModelRegistry(&now)
	absent := voterAddr(1)
	present := voterAddr(2)
	reg.register(absent.String(), now-10_000)
	reg.register(present.String(), now-10_000)

	reg.suspend(absent.String())

	hooks := NewVoteHooks(k, nil, reg, publicRoutes{}, noValidators{})
	proposal := proposalAt(5, now)
	basis := hooks.freezeEligibilityOnce(ctx, proposal)
	require.Equal(t, uint64(1), basis.Denominator, "only the un-suspended controller is in the basis")

	now += 100
	ctx = ctx.WithBlockTime(time.Unix(now, 0))
	reg.reinstate(absent.String())

	err := hooks.recordBallot(ctx, proposal, absent.String(), absent.Bytes(), single(v1.OptionYes))
	require.Error(t, err, "a controller outside the frozen denominator must not enter the numerator")

	msg, broken := TurnoutWithinFrozenBasisInvariant(k)(ctx)
	require.False(t, broken, msg)
	require.LessOrEqual(t, k.GetRunningTally(ctx, proposal.Id).Turnout, basis.Denominator)
}

// Deleting a proposal's frozen basis while its ballots survive is the other way the numerator escapes its denominator, and the net has to see it as a break rather than as an empty proposal.
func TestNet_BallotsWithoutAFrozenBasisBreakTheInvariant(t *testing.T) {
	ctx, k := govSetup(t)
	now := int64(4_000_000)
	ctx = ctx.WithBlockTime(time.Unix(now, 0))

	reg := newModelRegistry(&now)
	addr := voterAddr(3)
	reg.register(addr.String(), now-10_000)

	k.RecordVote(ctx, 9, addr.Bytes(), int32(v1.OptionYes), true)

	msg, broken := TurnoutWithinFrozenBasisInvariant(k)(ctx)
	require.True(t, broken, "counted ballots with no frozen basis must break the invariant")
	require.Contains(t, msg, "NO frozen eligibility basis")
}
