# Closure capture differential tests

34 standalone programs; 102 sequential builds. All builds and runs succeeded. Stock Go 1.27.3, default fc, and fc built with FC_CAPREF=1 produced identical output for every case. No defeated byValCapture rule was found. Modules specify Go 1.25. Programs and build logs reside in each case directory alongside this report; results.jsonl contains machine-readable results. No compiler or repository files were modified.

Each program was built with CGO_ENABLED=0, GOTOOLCHAIN=local, and GOFLAGS=-buildvcs=false. Stock GOCACHE: /home/exedev/targets/byval-review/gocache-stock. FC GOCACHE: /home/exedev/targets/fc-gocache. FC builds used /home/exedev/simd-lang/build/fcx build -C CASE -o CASE/objs-MODE -link CASE/bin-MODE .

## 01_short_redeclare — MATCH

stock: `{'build_exit': 0, 'seconds': 2.36, 'exit': 0, 'output': '2 3\n'}`

fc: `{'build_exit': 0, 'seconds': 0.55, 'exit': 0, 'output': '2 3\n'}`

capref: `{'build_exit': 0, 'seconds': 0.67, 'exit': 0, 'output': '2 3\n'}`


## 02_select_assign — MATCH

stock: `{'build_exit': 0, 'seconds': 0.23, 'exit': 0, 'output': '2\n'}`

fc: `{'build_exit': 0, 'seconds': 0.73, 'exit': 0, 'output': '2\n'}`

capref: `{'build_exit': 0, 'seconds': 0.65, 'exit': 0, 'output': '2\n'}`


## 03_select_short_shadow — MATCH

stock: `{'build_exit': 0, 'seconds': 0.2, 'exit': 0, 'output': '2 true\n1\n'}`

fc: `{'build_exit': 0, 'seconds': 0.63, 'exit': 0, 'output': '2 true\n1\n'}`

capref: `{'build_exit': 0, 'seconds': 0.4, 'exit': 0, 'output': '2 true\n1\n'}`


## 04_type_switch — MATCH

stock: `{'build_exit': 0, 'seconds': 0.12, 'exit': 0, 'output': '1 2\n'}`

fc: `{'build_exit': 0, 'seconds': 0.37, 'exit': 0, 'output': '1 2\n'}`

capref: `{'build_exit': 0, 'seconds': 0.44, 'exit': 0, 'output': '1 2\n'}`


## 05_range_func_assign — MATCH

stock: `{'build_exit': 0, 'seconds': 0.11, 'exit': 0, 'output': '2\n'}`

fc: `{'build_exit': 0, 'seconds': 0.39, 'exit': 0, 'output': '2\n'}`

capref: `{'build_exit': 0, 'seconds': 0.4, 'exit': 0, 'output': '2\n'}`


## 06_range_func_shared — MATCH

stock: `{'build_exit': 0, 'seconds': 0.12, 'exit': 0, 'output': '2\n'}`

fc: `{'build_exit': 0, 'seconds': 0.38, 'exit': 0, 'output': '2\n'}`

capref: `{'build_exit': 0, 'seconds': 0.42, 'exit': 0, 'output': '2\n'}`


## 07_named_result_defer — MATCH

stock: `{'build_exit': 0, 'seconds': 0.15, 'exit': 0, 'output': '2\n3\n'}`

fc: `{'build_exit': 0, 'seconds': 0.4, 'exit': 0, 'output': '2\n3\n'}`

capref: `{'build_exit': 0, 'seconds': 0.39, 'exit': 0, 'output': '2\n3\n'}`


## 08_two_levels_assign — MATCH

stock: `{'build_exit': 0, 'seconds': 0.19, 'exit': 0, 'output': '2\n'}`

fc: `{'build_exit': 0, 'seconds': 0.41, 'exit': 0, 'output': '2\n'}`

capref: `{'build_exit': 0, 'seconds': 0.41, 'exit': 0, 'output': '2\n'}`


## 09_method_call_chan — MATCH

stock: `{'build_exit': 0, 'seconds': 0.17, 'exit': 0, 'output': '2\n'}`

fc: `{'build_exit': 0, 'seconds': 0.43, 'exit': 0, 'output': '2\n'}`

capref: `{'build_exit': 0, 'seconds': 0.42, 'exit': 0, 'output': '2\n'}`


## 09_method_call_func — MATCH

stock: `{'build_exit': 0, 'seconds': 0.16, 'exit': 0, 'output': '2\n'}`

fc: `{'build_exit': 0, 'seconds': 0.39, 'exit': 0, 'output': '2\n'}`

capref: `{'build_exit': 0, 'seconds': 0.43, 'exit': 0, 'output': '2\n'}`


## 09_method_call_map — MATCH

stock: `{'build_exit': 0, 'seconds': 0.19, 'exit': 0, 'output': '2\n'}`

fc: `{'build_exit': 0, 'seconds': 0.43, 'exit': 0, 'output': '2\n'}`

capref: `{'build_exit': 0, 'seconds': 0.38, 'exit': 0, 'output': '2\n'}`


## 10_method_value_chan — MATCH

stock: `{'build_exit': 0, 'seconds': 0.2, 'exit': 0, 'output': '2\n'}`

fc: `{'build_exit': 0, 'seconds': 0.43, 'exit': 0, 'output': '2\n'}`

capref: `{'build_exit': 0, 'seconds': 0.4, 'exit': 0, 'output': '2\n'}`


## 10_method_value_func — MATCH

stock: `{'build_exit': 0, 'seconds': 0.17, 'exit': 0, 'output': '2\n'}`

fc: `{'build_exit': 0, 'seconds': 0.42, 'exit': 0, 'output': '2\n'}`

capref: `{'build_exit': 0, 'seconds': 0.42, 'exit': 0, 'output': '2\n'}`


## 10_method_value_map — MATCH

stock: `{'build_exit': 0, 'seconds': 0.16, 'exit': 0, 'output': '2\n'}`

fc: `{'build_exit': 0, 'seconds': 0.4, 'exit': 0, 'output': '2\n'}`

capref: `{'build_exit': 0, 'seconds': 0.4, 'exit': 0, 'output': '2\n'}`


## 11_goto_declaration — MATCH

stock: `{'build_exit': 0, 'seconds': 0.14, 'exit': 0, 'output': '0 1 2\n'}`

fc: `{'build_exit': 0, 'seconds': 0.63, 'exit': 0, 'output': '0 1 2\n'}`

capref: `{'build_exit': 0, 'seconds': 0.57, 'exit': 0, 'output': '0 1 2\n'}`


## 12_loop_late_assign — MATCH

stock: `{'build_exit': 0, 'seconds': 0.25, 'exit': 0, 'output': '10 11 12\n'}`

fc: `{'build_exit': 0, 'seconds': 0.63, 'exit': 0, 'output': '10 11 12\n'}`

capref: `{'build_exit': 0, 'seconds': 0.47, 'exit': 0, 'output': '10 11 12\n'}`


## 13_generic_pointer — MATCH

stock: `{'build_exit': 0, 'seconds': 0.2, 'exit': 0, 'output': '2\n'}`

fc: `{'build_exit': 0, 'seconds': 0.56, 'exit': 0, 'output': '2\n'}`

capref: `{'build_exit': 0, 'seconds': 0.65, 'exit': 0, 'output': '2\n'}`


## 14_type_param_map — MATCH

stock: `{'build_exit': 0, 'seconds': 0.2, 'exit': 0, 'output': '2\n'}`

fc: `{'build_exit': 0, 'seconds': 0.54, 'exit': 0, 'output': '2\n'}`

capref: `{'build_exit': 0, 'seconds': 0.47, 'exit': 0, 'output': '2\n'}`


## 15_parameter_redeclare — MATCH

stock: `{'build_exit': 0, 'seconds': 0.14, 'exit': 0, 'output': '2\n'}`

fc: `{'build_exit': 0, 'seconds': 0.38, 'exit': 0, 'output': '2\n'}`

capref: `{'build_exit': 0, 'seconds': 0.38, 'exit': 0, 'output': '2\n'}`


## 16_receiver_assign — MATCH

stock: `{'build_exit': 0, 'seconds': 0.17, 'exit': 0, 'output': '2\n'}`

fc: `{'build_exit': 0, 'seconds': 0.39, 'exit': 0, 'output': '2\n'}`

capref: `{'build_exit': 0, 'seconds': 0.38, 'exit': 0, 'output': '2\n'}`


## 17_labeled_continue — MATCH

stock: `{'build_exit': 0, 'seconds': 0.17, 'exit': 0, 'output': '10 11 12\n'}`

fc: `{'build_exit': 0, 'seconds': 0.38, 'exit': 0, 'output': '10 11 12\n'}`

capref: `{'build_exit': 0, 'seconds': 0.39, 'exit': 0, 'output': '10 11 12\n'}`


## 18_labeled_break_range — MATCH

stock: `{'build_exit': 0, 'seconds': 0.16, 'exit': 0, 'output': '2\n'}`

fc: `{'build_exit': 0, 'seconds': 0.39, 'exit': 0, 'output': '2\n'}`

capref: `{'build_exit': 0, 'seconds': 0.43, 'exit': 0, 'output': '2\n'}`


## 19_range_func_literal_local — MATCH

stock: `{'build_exit': 0, 'seconds': 0.15, 'exit': 0, 'output': '11 12\n'}`

fc: `{'build_exit': 0, 'seconds': 0.36, 'exit': 0, 'output': '11 12\n'}`

capref: `{'build_exit': 0, 'seconds': 0.36, 'exit': 0, 'output': '11 12\n'}`


## 20_range_func_read_shared — MATCH

stock: `{'build_exit': 0, 'seconds': 0.14, 'exit': 0, 'output': '1 1\n1\n'}`

fc: `{'build_exit': 0, 'seconds': 0.36, 'exit': 0, 'output': '1 1\n1\n'}`

capref: `{'build_exit': 0, 'seconds': 0.4, 'exit': 0, 'output': '1 1\n1\n'}`


## 21_named_result_range_return — MATCH

stock: `{'build_exit': 0, 'seconds': 0.12, 'exit': 0, 'output': '2\n2\n'}`

fc: `{'build_exit': 0, 'seconds': 0.36, 'exit': 0, 'output': '2\n2\n'}`

capref: `{'build_exit': 0, 'seconds': 0.39, 'exit': 0, 'output': '2\n2\n'}`


## 22_nested_addr — MATCH

stock: `{'build_exit': 0, 'seconds': 0.15, 'exit': 0, 'output': '2\n'}`

fc: `{'build_exit': 0, 'seconds': 0.4, 'exit': 0, 'output': '2\n'}`

capref: `{'build_exit': 0, 'seconds': 0.42, 'exit': 0, 'output': '2\n'}`


## 23_for_init_rebind — MATCH

stock: `{'build_exit': 0, 'seconds': 0.13, 'exit': 0, 'output': '0 1 2\n'}`

fc: `{'build_exit': 0, 'seconds': 0.41, 'exit': 0, 'output': '0 1 2\n'}`

capref: `{'build_exit': 0, 'seconds': 0.37, 'exit': 0, 'output': '0 1 2\n'}`


## 24_range_assign — MATCH

stock: `{'build_exit': 0, 'seconds': 0.13, 'exit': 0, 'output': '2\n'}`

fc: `{'build_exit': 0, 'seconds': 0.36, 'exit': 0, 'output': '2\n'}`

capref: `{'build_exit': 0, 'seconds': 0.4, 'exit': 0, 'output': '2\n'}`


## 25_unsafe_pointer — MATCH

stock: `{'build_exit': 0, 'seconds': 0.15, 'exit': 0, 'output': '2\n'}`

fc: `{'build_exit': 0, 'seconds': 0.41, 'exit': 0, 'output': '2\n'}`

capref: `{'build_exit': 0, 'seconds': 0.42, 'exit': 0, 'output': '2\n'}`


## 26_nested_named_result — MATCH

stock: `{'build_exit': 0, 'seconds': 0.17, 'exit': 0, 'output': '2\n2\n'}`

fc: `{'build_exit': 0, 'seconds': 0.44, 'exit': 0, 'output': '2\n2\n'}`

capref: `{'build_exit': 0, 'seconds': 0.41, 'exit': 0, 'output': '2\n2\n'}`


## 27_range_func_deferred_set — MATCH

stock: `{'build_exit': 0, 'seconds': 0.14, 'exit': 0, 'output': '2\n'}`

fc: `{'build_exit': 0, 'seconds': 0.37, 'exit': 0, 'output': '2\n'}`

capref: `{'build_exit': 0, 'seconds': 0.39, 'exit': 0, 'output': '2\n'}`


## 28_generic_typeparam_pointer — MATCH

stock: `{'build_exit': 0, 'seconds': 0.14, 'exit': 0, 'output': '2\n'}`

fc: `{'build_exit': 0, 'seconds': 0.37, 'exit': 0, 'output': '2\n'}`

capref: `{'build_exit': 0, 'seconds': 0.43, 'exit': 0, 'output': '2\n'}`


## 29_select_nested_lhs — MATCH

stock: `{'build_exit': 0, 'seconds': 0.14, 'exit': 0, 'output': '2\n'}`

fc: `{'build_exit': 0, 'seconds': 0.39, 'exit': 0, 'output': '2\n'}`

capref: `{'build_exit': 0, 'seconds': 0.38, 'exit': 0, 'output': '2\n'}`


## 30_goto_type_switch — MATCH

stock: `{'build_exit': 0, 'seconds': 0.17, 'exit': 0, 'output': '0 1 2\n'}`

fc: `{'build_exit': 0, 'seconds': 0.44, 'exit': 0, 'output': '0 1 2\n'}`

capref: `{'build_exit': 0, 'seconds': 0.43, 'exit': 0, 'output': '0 1 2\n'}`

