param(
    [Parameter(Mandatory=$true)][string]$OriginalPath,
    [Parameter(Mandatory=$true)][string]$PatchedPath,
    [Parameter(Mandatory=$true)][string]$CecilPath
)
$ErrorActionPreference = 'Stop'
Add-Type -Path $CecilPath
$original = [Mono.Cecil.ModuleDefinition]::ReadModule((Resolve-Path -LiteralPath $OriginalPath).Path)
$patched = [Mono.Cecil.ModuleDefinition]::ReadModule((Resolve-Path -LiteralPath $PatchedPath).Path)
try {
    function All-Types($types) { foreach ($type in $types) { $type; All-Types $type.NestedTypes } }
    function Method-Snapshot($method) {
        $lines = @($method.FullName, $method.Attributes.ToString(), $method.ImplAttributes.ToString())
        if ($method.HasBody) {
            $lines += $method.Body.InitLocals.ToString()
            $lines += @($method.Body.Variables | ForEach-Object { $_.VariableType.FullName })
            $lines += @($method.Body.Instructions | ForEach-Object ToString)
            $lines += @($method.Body.ExceptionHandlers | ForEach-Object ToString)
        }
        return $lines -join "`n"
    }
    $beforeTypes = @(All-Types $original.Types)
    $afterTypes = @(All-Types $patched.Types)
    if (($beforeTypes.FullName -join "`n") -cne ($afterTypes.FullName -join "`n")) { throw 'Types changed' }
    $changed = @()
    $count = 0
    for ($t=0; $t -lt $beforeTypes.Count; $t++) {
        $left=$beforeTypes[$t]; $right=$afterTypes[$t]
        if (($left.Fields.FullName -join "`n") -cne ($right.Fields.FullName -join "`n") -or $left.Methods.Count -ne $right.Methods.Count) { throw 'Members changed' }
        for ($i=0; $i -lt $left.Methods.Count; $i++) {
            if ((Method-Snapshot $left.Methods[$i]) -cne (Method-Snapshot $right.Methods[$i])) { $changed += $left.Methods[$i].FullName }
            $count++
        }
    }
    if ($changed.Count -ne 1 -or $changed[0] -cne 'System.Void Dev::OnDev()') { throw "Unexpected method changes: $changed" }
    $method = ($patched.Types | Where-Object FullName -eq Dev).Methods | Where-Object Name -eq OnDev
    $code = $method.Body.Instructions
    # Interpret the actual OnDev IL with stubbed manager/dialog calls. This
    # verifies branch targets and stack use without loading Unity on Windows.
    function Run-Click([long]$uid, [bool]$exists, [int]$locked, [int]$fame) {
        $stack = [Collections.Generic.List[object]]::new()
        $locals = @{}
        $events = [Collections.Generic.List[string]]::new()
        function Push($value) { $stack.Add($value) }
        function Pop { if (!$stack.Count) { throw 'IL stack underflow' }; $v=$stack[$stack.Count-1]; $stack.RemoveAt($stack.Count-1); return $v }
        $pc=0
        for ($steps=0; $steps -lt 150; $steps++) {
            $ins=$code[$pc]; $pc++
            switch ($ins.OpCode.Name) {
                'ldarg.0' { Push 'dev' }
                'ldfld' {
                    $null=Pop
                    switch ($ins.Operand.Name) {
                        'm_fame_up' { Push $fame }
                        'is_lock' { Push $locked }
                        default { Push $ins.Operand.Name }
                    }
                }
                'ldloc' { Push $locals[$ins.Operand.Index] }
                'stloc' { $locals[$ins.Operand.Index] = Pop }
                'ldc.i4.0' { Push 0 }
                'ldc.i4.2' { Push 2 }
                'ldc.i4' { Push $ins.Operand }
                'ldc.i4.s' { Push $ins.Operand }
                'ldstr' { Push $ins.Operand }
                'newarr' { $null=Pop; Push 'args' }
                'brtrue' { if (Pop) { $pc=$code.IndexOf($ins.Operand) } }
                'brfalse' { if (!(Pop)) { $pc=$code.IndexOf($ins.Operand) } }
                'bgt' { $b=Pop; $a=Pop; if ($a -gt $b) { $pc=$code.IndexOf($ins.Operand) } }
                { $_ -in 'call','callvirt' } {
                    $args=@(); for($j=0;$j -lt $ins.Operand.Parameters.Count;$j++) { $args+=,(Pop) }
                    if ($ins.Operand.HasThis) { $null=Pop }
                    switch ($ins.Operand.Name) {
                        'get_devBaseUniqueID' { Push $uid }
                        'GetCardInfoFromUniqID' { Push ([int]$exists) }
                        'get_Instance' { Push 'dialog' }
                        'format' { Push 'select card' }
                        'getString' { Push 'select fame' }
                        'openErrorDialogDefault' {
                            if ($args[0] -ne 2) { throw 'Dialog must only close' }
                            $events.Add('dialog:'+$args[1])
                        }
                        'SendCardFameStartTrain' { $events.Add('send') }
                        default { throw "Unsupported call: $($ins.Operand)" }
                    }
                }
                'ret' { if ($stack.Count) { throw 'IL stack not empty on return' }; return $events -join '|' }
                default { throw "Unsupported instruction: $ins" }
            }
        }
        throw 'OnDev did not terminate'
    }
    $prompt='dialog:卡牌已锁定，请先解锁后再进行名声培养。'
    $cases=@(
        @(0,$false,0,4,'dialog:select card'),
        @(2,$true,1,4,$prompt),
        @(2,$true,1,0,$prompt),
        @(2,$true,1,-1,$prompt),
        @(2,$true,2,4,$prompt),
        @(2,$true,0,4,'send'),
        @(2,$true,0,0,'dialog:select fame'),
        @(2,$true,0,-1,'dialog:select fame'),
        @(2,$false,0,4,'send')
    )
    foreach($case in $cases) {
        $actual=Run-Click $case[0] $case[1] $case[2] $case[3]
        if ($actual -cne $case[4]) { throw "Unexpected click result: $actual; expected $($case[4])" }
    }
    Write-Output "PASS: $count methods compared; only Dev.OnDev changed; $($cases.Count) click cases passed (IL simulation, not device verification)"
} finally { $original.Dispose(); $patched.Dispose() }
