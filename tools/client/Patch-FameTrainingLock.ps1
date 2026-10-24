param(
    [Parameter(Mandatory=$true)][string]$AssemblyPath,
    [Parameter(Mandatory=$true)][string]$CecilPath,
    [Parameter(Mandatory=$true)][string]$OutputPath
)
$ErrorActionPreference = 'Stop'
Add-Type -Path $CecilPath
$inputPath = (Resolve-Path -LiteralPath $AssemblyPath).Path
$output = [IO.Path]::GetFullPath($OutputPath)
if ($inputPath -eq $output -or (Test-Path -LiteralPath $output)) { throw 'Output must be a new file, different from input' }
$module = [Mono.Cecil.ModuleDefinition]::ReadModule($inputPath)
try {
    function Find-Type([string]$name) {
        $found = @($module.Types | Where-Object FullName -eq $name)
        if ($found.Count -ne 1) { throw "Expected one type: $name" }
        return $found[0]
    }
    function Find-Method($type, [string]$name) {
        $found = @($type.Methods | Where-Object Name -eq $name)
        if ($found.Count -ne 1) { throw "Expected one method: $name" }
        return $found[0]
    }
    function Ins([string]$op, $operand=$null) {
        $opcode = [Mono.Cecil.Cil.OpCodes]::$op
        if ($null -eq $operand) { return [Mono.Cecil.Cil.Instruction]::Create($opcode) }
        return [Mono.Cecil.Cil.Instruction]::Create($opcode, $operand)
    }
    $dev = Find-Type 'Dev'
    $method = Find-Method $dev 'OnDev'
    $original = @($method.Body.Instructions)
    if (@($original | Where-Object { $_.Operand -is [Mono.Cecil.FieldReference] -and $_.Operand.Name -eq 'is_lock' }).Count) { throw 'OnDev already checks card lock' }
    # Reuse the existing no-selection branch and dialog API. Insert before
    # fame validation, so a locked card always explains how to proceed.
    $selection = @($original | Where-Object { $_.Operand -is [Mono.Cecil.MethodReference] -and $_.Operand.Name -eq 'get_devBaseUniqueID' })
    $send = @($original | Where-Object { $_.Operand -is [Mono.Cecil.MethodReference] -and $_.Operand.Name -eq 'SendCardFameStartTrain' })
    if ($selection.Count -ne 1 -or $send.Count -ne 1 -or $selection[0].Next.OpCode.Name -ne 'brtrue') { throw 'Unsupported OnDev selection layout' }
    $anchor = $selection[0].Next.Operand
    if ($anchor.OpCode.Name -ne 'ldarg.0' -or $anchor.Next.Operand.Name -ne 'm_fame_up') { throw 'Unsupported fame validation entry' }
    $dialogInstance = @($original | Where-Object { $_.Operand -is [Mono.Cecil.MethodReference] -and $_.Operand.FullName -match 'TSingleton`1<DialogMgr>::get_Instance' })[0].Operand
    $openDialog = @($original | Where-Object { $_.Operand -is [Mono.Cecil.MethodReference] -and $_.Operand.FullName -eq 'System.Void DialogMgr::openErrorDialogDefault(System.String,DialogMgr/ERROR_DIALOG_CB)' })[0].Operand
    $close = (Find-Type 'DialogMgr').NestedTypes | Where-Object Name -eq 'ERROR_DIALOG_CB' | ForEach-Object { $_.Fields | Where-Object Name -eq CLOSE }
    if ($null -eq $dialogInstance -or $null -eq $openDialog -or $close.Constant -ne 2) { throw 'Unsupported close-only dialog API' }
    $card = Find-Type 'proto.CardInfo'
    $local = [Mono.Cecil.Cil.VariableDefinition]::new($card)
    $method.Body.Variables.Add($local)
    $method.Body.InitLocals = $true
    $patch = [Collections.Generic.List[Mono.Cecil.Cil.Instruction]]::new()
    function Emit([string]$op, $operand=$null) { $patch.Add((Ins $op $operand)) }
    Emit 'Ldarg_0'
    Emit 'Ldfld' ($dev.Fields | Where-Object Name -eq 'm_card_mgr')
    Emit 'Ldarg_0'
    Emit 'Ldfld' ($dev.Fields | Where-Object Name -eq 'm_card_dev')
    Emit 'Ldfld' ((Find-Type 'CardDev').Fields | Where-Object Name -eq 'data_pool')
    Emit 'Callvirt' $selection[0].Operand
    Emit 'Callvirt' (Find-Method (Find-Type 'CardMgr') 'GetCardInfoFromUniqID')
    Emit 'Stloc' $local
    Emit 'Ldloc' $local
    Emit 'Brfalse' $anchor
    Emit 'Ldloc' $local
    Emit 'Ldfld' ($card.Fields | Where-Object Name -eq 'is_lock')
    Emit 'Brfalse' $anchor
    Emit 'Call' $dialogInstance
    Emit 'Ldstr' '卡牌已锁定，请先解锁后再进行名声培养。'
    Emit 'Ldc_I4_2'
    Emit 'Callvirt' $openDialog
    Emit 'Ret'
    # Existing selection branches must enter the new check, not skip it.
    foreach ($instruction in $original) {
        if ([Object]::ReferenceEquals($instruction.Operand, $anchor)) { $instruction.Operand = $patch[0] }
    }
    $il = $method.Body.GetILProcessor()
    foreach ($instruction in $patch) { $il.InsertBefore($anchor, $instruction) }
    $method.Body.MaxStackSize = [Math]::Max(3, $method.Body.MaxStackSize)
    $module.Write($output)
    Write-Output "Patched Dev.OnDev lock prompt: $output"
} finally { $module.Dispose() }
