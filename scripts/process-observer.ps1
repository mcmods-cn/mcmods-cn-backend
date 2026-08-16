param(
  [Parameter(Mandatory = $true)][int]$ProcessId,
  [int]$DurationSeconds = 30,
  [string]$Output = ""
)

$logicalProcessors = [Environment]::ProcessorCount
$previousCPU = (Get-Process -Id $ProcessId).TotalProcessorTime.TotalSeconds
$peakMemory = 0L
$peakCPU = 0.0
$samples = 0
for ($index = 0; $index -lt ($DurationSeconds * 2); $index++) {
  Start-Sleep -Milliseconds 500
  $process = Get-Process -Id $ProcessId -ErrorAction SilentlyContinue
  if (-not $process) { break }
  $cpu = (($process.TotalProcessorTime.TotalSeconds - $previousCPU) / 0.5 / $logicalProcessors * 100)
  $previousCPU = $process.TotalProcessorTime.TotalSeconds
  $peakCPU = [Math]::Max($peakCPU, $cpu)
  $peakMemory = [Math]::Max($peakMemory, $process.WorkingSet64)
  $samples++
}
$report = [pscustomobject]@{
  samples = $samples
  durationSeconds = $DurationSeconds
  peakCpuPercent = [Math]::Round($peakCPU, 2)
  peakWorkingSetMB = [Math]::Round($peakMemory / 1MB, 2)
}
$json = $report | ConvertTo-Json
if ($Output) { Set-Content -LiteralPath $Output -Value $json -Encoding UTF8 }
$json
