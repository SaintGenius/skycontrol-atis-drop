# Sky Control ATIS drop

Copy these files ON TOP of:

`C:\Users\Rob\Downloads\skycontrol\skycontrol\`

Keep the folder names. Then **BUILD.bat** → **LAUNCH.bat**.

Native radio is unchanged (tower/ground still on 261 at Senaki).

## What this adds

- Second Native SRS radio named **Information**
- Senaki ATIS = **260.900** (0.100 below tower 261.000)
- Ground no longer guesses 261 for ATIS

## What you should see at start

```
  ATIS transmitter: on (nearest field, COM2)
  ATIS: Senaki Information on 260.900  (tune COM2)
```

and later:

```
  SRS native TX 260.900 AM as Senaki Information
```

## Test

1. Sit in the jet at Senaki. COM1 = **261.000**
2. `Radio check` — Ground still answers on 261
3. `Senaki Ground, what is the ATIS frequency?`
   — answer should be **two six zero decimal niner**, not 261
4. Tune **COM2 to 260.900**, listen. Piper ATIS loop.
5. Optional: type `atis` in the Sky Control window

Do not change `config.yaml`. Do not replace `native.go`.
